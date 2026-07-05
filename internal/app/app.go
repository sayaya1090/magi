// Package app wires the ports together into the application service: it turns
// commands into persisted events and a live event stream (CQRS-lite, DESIGN §4).
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// App is the application service implementing the command/event boundary.
type App struct {
	store            port.Store
	llm              port.LLMProvider
	providers        map[string]port.LLMProvider // named LLM profiles (per-agent endpoint/key routing)
	profileDefs      map[string]ProfileDef       // profile definitions (guarded by mu), for the /route editor
	routeOverrides   map[string]routeOverride    // runtime per-agent routing edits (guarded by mu)
	tools            port.ToolRegistry
	bus              *bus.Bus
	plat             port.Platform
	cfg              Config
	contextProviders []port.ContextProvider // RAG-like context injectors

	mu        sync.Mutex
	wg        sync.WaitGroup                               // tracks run + dispatch goroutines for graceful Close
	closed    bool                                         // set by Close: no new run/dispatch goroutines (no Add after Wait)
	cancels   map[session.SessionID]context.CancelFunc     // in-flight runs (Interrupt)
	perms     map[session.SessionID]map[string]chan string // pending permission decisions
	questions map[session.SessionID]map[string]chan string // pending ask_user picks by call id
	grants    map[session.SessionID]map[string]bool        // "always" grants per tool
	sessions  map[session.SessionID]session.Session        // session metadata cache

	sem        chan struct{} // concurrency limiter for subagents (D7)
	spawnCount atomic.Int64  // cumulative subagents spawned (runaway backstop)

	lastActivity sync.Map                          // session.SessionID -> time.Time (liveness for the sidecar health check)
	bg           map[session.SessionID]*bgGroup    // per-parent background subagent tracking
	pendingAsks  map[session.SessionID]chan string // parent session -> channel for a subagent's pending ask answer
	reports      map[session.SessionID]*subReport  // subagent session -> filed final report (guarded by mu)

	memMu      sync.Mutex
	memCache   map[string]string       // workdir -> durable AGENTS.md memory
	skillCache map[string][]port.Skill // workdir -> loaded skills

	permPolicy string  // runtime-adjustable permission policy (guarded by mu)
	policy     *Policy // guardrail rules engine (deny floor, allow rules, bash scan)

	lastPromptTokens      map[session.SessionID]int            // real prompt_tokens from last turn (guarded by mu)
	todos                 map[session.SessionID][]session.Todo // per-session plan (guarded by mu)
	stage                 map[session.SessionID]string         // current loop stage for event tagging (guarded by mu)
	criteria              map[session.SessionID]string         // elicited acceptance criteria per turn (guarded by mu)
	estSteps              map[session.SessionID]int            // planner's advisory step estimate per turn (guarded by mu)
	autoOrchestrateActive map[session.SessionID]bool           // whether auto-orchestration has been triggered for this session (guarded by mu)
	probingWindows        map[string]struct{}                  // models whose context window is being (or was) lazily probed (guarded by mu)
}

// New constructs an App.
func New(store port.Store, llm port.LLMProvider, tools port.ToolRegistry, b *bus.Bus, plat port.Platform, cfg Config) *App {
	c := cfg.withDefaults()
	c.ProfileModels = cloneStringMap(c.ProfileModels) // runtime edits must not mutate the caller's map
	return &App{
		store:                 store,
		llm:                   llm,
		providers:             cloneProviders(c.Providers),
		profileDefs:           cloneProfileDefs(c.ProfileDefs),
		routeOverrides:        map[string]routeOverride{},
		tools:                 tools,
		bus:                   b,
		plat:                  plat,
		cfg:                   c,
		cancels:               map[session.SessionID]context.CancelFunc{},
		perms:                 map[session.SessionID]map[string]chan string{},
		questions:             map[session.SessionID]map[string]chan string{},
		grants:                map[session.SessionID]map[string]bool{},
		sessions:              map[session.SessionID]session.Session{},
		sem:                   make(chan struct{}, c.Concurrency),
		permPolicy:            c.Permission,
		policy:                newPolicy(c.Allow, c.Deny, c.AllowDomains),
		lastPromptTokens:      map[session.SessionID]int{},
		todos:                 map[session.SessionID][]session.Todo{},
		stage:                 map[session.SessionID]string{},
		criteria:              map[session.SessionID]string{},
		estSteps:              map[session.SessionID]int{},
		bg:                    map[session.SessionID]*bgGroup{},
		pendingAsks:           map[session.SessionID]chan string{},
		reports:               map[session.SessionID]*subReport{},
		autoOrchestrateActive: map[session.SessionID]bool{},
		probingWindows:        map[string]struct{}{},
	}
}

// subReport is a subagent's filed final result (the explicit output contract).
type subReport struct {
	summary, status, details string
}

// reportStatusPrefix leads every report frame subReport.result emits: a single
// "STATUS: <WORD>" line the orchestrator and planner parse to tell done from blocked/failed.
const reportStatusPrefix = "STATUS: "

// reportStatusWord extracts the status token of a report frame's leading "STATUS: <WORD>" line
// (upper-cased), or "" when line (trimmed) is not exactly that frame — the single recognizer
// behind refineReportsFailure and stripReportStatus. The "STATUS:" keyword is matched
// case-insensitively; the emitted frame is always upper-case, so this only widens tolerance for
// free-typed model text.
func reportStatusWord(line string) string {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 2 && strings.EqualFold(f[0], strings.TrimSpace(reportStatusPrefix)) {
		return strings.ToUpper(f[1])
	}
	return ""
}

// fileReport records a subagent's final report once; later calls in the same
// turn are rejected so a model can't spam it. (output side of the contract)
func (a *App) fileReport(sid session.SessionID, summary, status, details string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reports[sid] != nil {
		return fmt.Errorf("you already filed a report this turn; your turn is ending")
	}
	a.reports[sid] = &subReport{summary: summary, status: status, details: details}
	return nil
}

// takeReport returns and clears any report filed for a session.
func (a *App) takeReport(sid session.SessionID) *subReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.reports[sid]
	delete(a.reports, sid)
	return r
}

// result renders the subagent's result around the given answer body, leading with
// the status so the orchestrator can tell done from blocked/failed at a glance.
func (r *subReport) result(answer string) string {
	out := reportStatusPrefix + strings.ToUpper(r.status) + "\n" + strings.TrimSpace(answer)
	if d := strings.TrimSpace(r.details); d != "" && !strings.Contains(answer, d) {
		out += "\n\n" + d
	}
	return out
}

// Rewind removes the last n user turns from a session by truncating its event
// log, and clears derived per-session state. Returns the new highest seq.
func (a *App) Rewind(ctx context.Context, sid session.SessionID, n int) (int64, error) {
	if n < 1 {
		n = 1
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return 0, err
	}
	var promptSeqs []int64
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			promptSeqs = append(promptSeqs, e.Seq)
		}
	}
	if len(promptSeqs) == 0 {
		return 0, fmt.Errorf("nothing to rewind")
	}
	if n > len(promptSeqs) {
		n = len(promptSeqs)
	}
	boundary := promptSeqs[len(promptSeqs)-n] - 1 // keep everything before that prompt
	if err := a.store.Truncate(ctx, sid, boundary); err != nil {
		return 0, err
	}
	a.mu.Lock()
	delete(a.lastPromptTokens, sid)
	delete(a.todos, sid)
	delete(a.stage, sid)
	delete(a.criteria, sid)
	delete(a.estSteps, sid)
	a.mu.Unlock()
	return boundary, nil
}

// SessionState returns a resumed session's reconstructed messages and the
// highest seq seen (so a UI can subscribe for only newer events).
func (a *App) SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, 0, err
	}
	var last int64
	for _, e := range evs {
		if e.Seq > last {
			last = e.Seq
		}
	}
	return reconstruct(evs), last, nil
}

// Todos returns a session's current plan.
func (a *App) Todos(sid session.SessionID) []session.Todo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.todos[sid]
}

// PlanChildren returns the child sessions spawned to carry out the given plan step
// of parent, in creation order. It joins the parent link (session.Parent) with the
// per-child ParentStep edge recorded at spawn — the pair reconstructs the plan tree
// so a child's own todos can render indented under this step. Empty when the step was
// solo or its delegate/refine child never registered a sub-plan.
func (a *App) PlanChildren(parent session.SessionID, step int) []session.SessionID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var kids []session.Session
	for _, s := range a.sessions {
		if s.Parent == parent && s.ParentStep != nil && *s.ParentStep == step {
			kids = append(kids, s)
		}
	}
	sort.Slice(kids, func(i, j int) bool {
		if !kids[i].Created.Equal(kids[j].Created) {
			return kids[i].Created.Before(kids[j].Created)
		}
		return kids[i].ID < kids[j].ID // stable tie-break for same-instant spawns
	})
	out := make([]session.SessionID, len(kids))
	for i, s := range kids {
		out[i] = s.ID
	}
	return out
}

// realPromptTokens returns the actual prompt token count from the last turn (0
// if not yet known).
func (a *App) realPromptTokens(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastPromptTokens[sid]
}

func (a *App) setPromptTokens(sid session.SessionID, n int) {
	a.mu.Lock()
	a.lastPromptTokens[sid] = n
	a.mu.Unlock()
}

// AgentNames returns the configured subagent names, sorted.
func (a *App) AgentNames() []string {
	names := make([]string, 0, len(a.cfg.Agents))
	for n := range a.cfg.Agents {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ToolNames returns the names of all registered tools, sorted.
func (a *App) ToolNames() []string {
	tools := a.tools.List()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}

// GitDiff returns the complete working-tree diff for workdir (empty if no
// changes), INCLUDING the content of new untracked files. A plain `git diff`
// omits untracked files, which hides exactly the new files an agent most often
// creates — and starves the council (the termination gate) of the evidence it
// needs to confirm the work, so it keeps voting "continue". To include them
// without disturbing the user's index, everything is staged into a throwaway
// index (GIT_INDEX_FILE) and diffed against HEAD; the real index is untouched.
func (a *App) GitDiff(ctx context.Context, workdir string) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}

	// Complete diff via a throwaway index, so new files show up with content.
	if idx, err := os.CreateTemp("", "magi-diff-index-*"); err == nil {
		idxPath := idx.Name()
		idx.Close()
		os.Remove(idxPath) // git recreates it; we only needed a unique, unused path
		defer os.Remove(idxPath)
		env := []string{"GIT_INDEX_FILE=" + idxPath}
		add, aerr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"add", "-A"}, Dir: workdir, Env: env})
		if aerr == nil && add.ExitCode == 0 {
			// HEAD when there is history, the empty-tree object otherwise (fresh repo
			// with no commits), so every staged file shows as an addition.
			against := "HEAD"
			if rev, rerr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"rev-parse", "--verify", "-q", "HEAD"}, Dir: workdir}); rerr != nil || rev.ExitCode != 0 {
				against = emptyTreeRef
			}
			diff, derr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"diff", "--cached", against}, Dir: workdir, Env: env})
			if derr == nil && diff.ExitCode == 0 {
				return string(diff.Stdout), nil
			}
		}
	}

	// Fallback (temp index unavailable, or not a git repo): plain working-tree
	// diff, then a status summary if the diff is empty but untracked files exist.
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"diff"}, Dir: workdir})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = "git diff failed"
		}
		return "", fmt.Errorf("%s", msg)
	}
	if strings.TrimSpace(string(res.Stdout)) != "" {
		return string(res.Stdout), nil
	}
	st, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"status", "--short"}, Dir: workdir})
	if err == nil && strings.TrimSpace(string(st.Stdout)) != "" {
		return string(st.Stdout), nil
	}
	return "", nil
}

// emptyTreeRef is git's well-known empty-tree object hash — a stable base to diff
// against in a repository that has no commits yet.
const emptyTreeRef = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Permission returns the current tool-permission policy.
func (a *App) Permission() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.permPolicy
}

// SetPermission updates the permission policy at runtime (ask|auto|allow|deny).
func (a *App) SetPermission(p string) {
	a.mu.Lock()
	a.permPolicy = p
	a.mu.Unlock()
}

// agentFor returns the AgentSpec for a session, falling back to a default built
// from the global system prompt with access to all tools.
func (a *App) agentFor(s session.Session) AgentSpec {
	if spec, ok := a.resolveAgentSpec(s.Agent); ok {
		return spec
	}
	return AgentSpec{Name: orDefault(s.Agent, "default"), System: a.cfg.System}
}

// resolveAgentSpec looks up an agent's configured spec and applies any runtime
// routing override (from the /route menu). Used by both agentFor (top-level) and
// spawn (subagents) so overrides take effect everywhere.
func (a *App) resolveAgentSpec(name string) (AgentSpec, bool) {
	spec, ok := a.cfg.Agents[name]
	if !ok {
		return AgentSpec{}, false
	}
	a.mu.Lock()
	ov, has := a.routeOverrides[name]
	a.mu.Unlock()
	if has {
		if ov.model != "" {
			spec.Model = session.ModelRef{Provider: "openai", Model: ov.model}
		}
		spec.Provider = ov.provider
	}
	return spec, true
}

// AgentRoutes returns each configured agent's current effective routing (model +
// profile), for the /route editor. Sorted by name.
func (a *App) AgentRoutes() []AgentRoute {
	names := a.AgentNames()
	out := make([]AgentRoute, 0, len(names))
	for _, n := range names {
		spec, _ := a.resolveAgentSpec(n)
		m := spec.Model.Model
		if m == "" {
			m = a.cfg.Model.Model // unrouted agents inherit the session/default model
		}
		out = append(out, AgentRoute{Name: n, Model: m, Provider: spec.Provider})
	}
	return out
}

// SetModel changes a session's active (default) model at runtime. Session-scoped:
// it updates the cached session so the next loop iteration uses it.
func (a *App) SetModel(sid session.SessionID, modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return
	}
	a.mu.Lock()
	if s, ok := a.sessions[sid]; ok {
		s.Model = session.ModelRef{Provider: "openai", Model: modelID}
		a.sessions[sid] = s
	}
	p := a.cfg.RoutePersister
	a.mu.Unlock()
	if p != nil {
		_ = p.PersistModel(modelID) // best-effort
	}
}

// SetAgentRoute applies a runtime routing edit for an agent. A value naming a
// configured profile routes the agent to that backend (provider+model); any
// other value is a bare model on the default backend; empty clears the override.
func (a *App) SetAgentRoute(name, value string) {
	value = strings.TrimSpace(value)
	a.mu.Lock()
	if value == "" {
		delete(a.routeOverrides, name)
	} else if mdl, isProfile := a.cfg.ProfileModels[value]; isProfile {
		a.routeOverrides[name] = routeOverride{model: mdl, provider: value}
	} else {
		a.routeOverrides[name] = routeOverride{model: value}
	}
	p := a.cfg.RoutePersister
	a.mu.Unlock()
	if p != nil {
		_ = p.PersistRoute(name, value) // best-effort
	}
}

// Profiles returns the defined LLM profiles, sorted by name, for the editor.
func (a *App) Profiles() []ProfileDef {
	a.mu.Lock()
	defer a.mu.Unlock()
	names := make([]string, 0, len(a.profileDefs))
	for n := range a.profileDefs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ProfileDef, 0, len(names))
	for _, n := range names {
		out = append(out, a.profileDefs[n])
	}
	return out
}

// SetProfile adds or updates a named LLM profile at runtime: it builds the
// provider (so routing to it works this session), records the definition, and
// persists it to [llm.profiles.<name>]. A no-op if the name is empty.
func (a *App) SetProfile(p ProfileDef) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return
	}
	a.mu.Lock()
	if a.profileDefs == nil {
		a.profileDefs = map[string]ProfileDef{}
	}
	a.profileDefs[p.Name] = p
	a.cfg.ProfileModels[p.Name] = p.Model
	if a.NewProviderFn() != nil {
		a.providers[p.Name] = a.cfg.NewProvider(p)
	}
	persist := a.cfg.RoutePersister
	a.mu.Unlock()
	if persist != nil {
		_ = persist.PersistProfile(p) // best-effort
	}
}

// NewProviderFn returns the configured provider factory (nil-safe helper).
func (a *App) NewProviderFn() ProviderFactory { return a.cfg.NewProvider }

func cloneProviders(m map[string]port.LLMProvider) map[string]port.LLMProvider {
	out := make(map[string]port.LLMProvider, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneProfileDefs(m map[string]ProfileDef) map[string]ProfileDef {
	out := make(map[string]ProfileDef, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// providerFor returns the LLM provider for an agent: its named profile's backend
// (per-agent endpoint/key routing) when set and registered, else the default.
func (a *App) providerFor(spec AgentSpec) port.LLMProvider {
	if spec.Provider != "" {
		if p := a.providers[spec.Provider]; p != nil {
			return p
		}
	}
	return a.llm
}

// CreateSession starts a new session and persists session.created.
func (a *App) CreateSession(ctx context.Context, c command.CreateSession) (session.SessionID, error) {
	sid := session.SessionID("s_" + newID())
	model := c.Model
	if model == (session.ModelRef{}) {
		model = a.cfg.Model
	}
	s := session.Session{
		ID:      sid,
		Workdir: c.Workdir,
		Agent:   c.Agent,
		Model:   model,
		Created: time.Now(),
	}
	a.mu.Lock()
	a.sessions[sid] = s
	a.mu.Unlock()

	data, _ := json.Marshal(event.SessionCreatedData{Workdir: c.Workdir, Agent: c.Agent, Model: model})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, c.Actor, data); err != nil {
		return "", err
	}
	return sid, nil
}

// Submit appends the user's prompt and starts the agent loop asynchronously.
func (a *App) Submit(ctx context.Context, c command.SubmitPrompt) error {
	// A new top-level request starts with a fresh plan — clear the previous
	// turn's todos so a stale (completed) plan doesn't linger in the UI; the
	// agent repopulates it via todowrite if the new task warrants one.
	a.SetTodos(c.SessionID, nil)
	a.mu.Lock()
	delete(a.criteria, c.SessionID) // new top-level prompt → drop cached criteria; re-elicited at the next gate (D15)
	delete(a.estSteps, c.SessionID) // …and the previous task's advisory step estimate
	a.mu.Unlock()
	if err := a.appendPrompt(ctx, c); err != nil {
		return err
	}
	a.startRun(ctx, c.SessionID)
	return nil
}

// Steer injects a user message into a session that is already running, so the
// in-flight agent picks it up at its next step (it re-reads the conversation
// each step) instead of the user having to wait for the turn to finish. If no
// turn is running, it behaves like Submit and starts one.
func (a *App) Steer(ctx context.Context, c command.SubmitPrompt) error {
	if err := a.appendPrompt(ctx, c); err != nil {
		return err
	}
	// Wake a loop parked in the background-subagent wait so it picks up this
	// steer immediately (otherwise it would sleep until a subagent finished).
	a.bgWake(c.SessionID)
	a.mu.Lock()
	running := a.cancels[c.SessionID] != nil
	a.mu.Unlock()
	if !running {
		a.startRun(ctx, c.SessionID) // turn already ended — process it now
	}
	return nil
}

// appendPrompt records a user prompt as a fact (shows in the transcript and is
// visible to the running loop's per-step re-read).
func (a *App) appendPrompt(ctx context.Context, c command.SubmitPrompt) error {
	// A user prompt/steer begins (or resumes) execution — reset the stage so it
	// isn't tagged with the prior turn's leftover stage (D15). System injections
	// (council/hooks/auto) append via appendFact directly and keep their stage.
	if c.Actor.Kind == event.ActorUser {
		a.setStage(c.SessionID, stageExecute)
	}
	msgID := "m_" + newID()
	data, _ := json.Marshal(event.PromptSubmittedData{MessageID: msgID, Parts: c.Parts})
	return a.appendFact(ctx, c.SessionID, event.TypePromptSubmitted, c.Actor, data)
}

// startRun launches the agent loop for a session unless one is already running
// (single run goroutine per session). After the loop ends it re-checks, under
// the lock, for a user message that was steered in during the exit window and
// runs again so nothing is stranded.
func (a *App) startRun(ctx context.Context, sid session.SessionID) {
	a.mu.Lock()
	if a.closed || a.cancels[sid] != nil {
		a.mu.Unlock()
		return // shutting down, or already running (the loop picks up steered input on re-read)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancels[sid] = cancel
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		defer cancel()
		for {
			err := a.run(runCtx, sid)
			a.mu.Lock()
			// Atomic with the delete: if a steer landed after the loop's last read
			// but before we tear down, run again rather than stranding it. But do NOT
			// re-run after a terminal error (e.g. a provider 429/5xx): the prompt is
			// still "unanswered", and re-running would hammer a failing/rate-limited
			// backend into an error storm. The error event already ended the turn.
			if err == nil && runCtx.Err() == nil && a.hasUnansweredUserPrompt(runCtx, sid) {
				a.mu.Unlock()
				continue
			}
			delete(a.cancels, sid)
			a.mu.Unlock()
			break
		}
		// On interruption the loop returns without a terminal event; emit one (on a
		// detached context, since runCtx is cancelled) so observers like the TUI
		// stop showing "working" instead of hanging forever.
		if runCtx.Err() != nil {
			d, _ := json.Marshal(event.TurnFinishedData{})
			_ = a.appendFact(context.WithoutCancel(runCtx), sid, event.TypeTurnFinished,
				event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
		}
	}()
}

// hasUnansweredUserPrompt reports whether the last message in the session is a
// user prompt with no assistant response after it (a steered-in message the
// agent has not yet handled).
func (a *App) hasUnansweredUserPrompt(ctx context.Context, sid session.SessionID) bool {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return false
	}
	msgs := reconstruct(evs)
	if len(msgs) == 0 {
		return false
	}
	return msgs[len(msgs)-1].Role == session.RoleUser
}

// Close cancels every in-flight run and background subagent, then waits for their
// goroutines to finish (bounded by ctx). This drains pending store writes before
// shutdown so they cannot race teardown — e.g. a test's temp-dir cleanup, which
// otherwise fails with "directory not empty" when a subagent appends after the
// test returns. Idempotent: safe to call more than once.
func (a *App) Close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true // stop new run/dispatch goroutines so wg.Add can't follow wg.Wait
	for _, cancel := range a.cancels {
		cancel()
	}
	a.mu.Unlock()
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Interrupt cancels the in-flight turn for a session.
func (a *App) Interrupt(ctx context.Context, c command.Interrupt) error {
	a.mu.Lock()
	cancel := a.cancels[c.SessionID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// RespondQuestion delivers the user's pick to a waiting ask_user execution.
func (a *App) RespondQuestion(ctx context.Context, c command.RespondQuestion) error {
	a.mu.Lock()
	ch := a.questions[c.SessionID][c.CallID]
	a.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending question for call %s", c.CallID)
	}
	select {
	case ch <- c.Answer:
	default:
	}
	return nil
}

// RespondPermission delivers a decision to a waiting tool execution.
func (a *App) RespondPermission(ctx context.Context, c command.RespondPermission) error {
	a.mu.Lock()
	ch := a.perms[c.SessionID][c.CallID]
	a.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending permission for call %s", c.CallID)
	}
	select {
	case ch <- c.Decision:
	default:
	}
	return nil
}

// Compact appends a compaction snapshot summarizing the conversation so far.
// (M1: summary generation is a stub; the mechanism + persistence are real.)
func (a *App) Compact(ctx context.Context, c command.Compact) error {
	evs, err := a.store.Read(ctx, c.SessionID, 0)
	if err != nil {
		return err
	}
	var upTo int64
	if n := len(evs); n > 0 {
		upTo = evs[n-1].Seq
	}
	msgs := reconstruct(evs)
	summary := summarize(msgs)
	// Manual compaction replaces everything up to upTo, so the post-state is just
	// the summary.
	data, _ := json.Marshal(event.CompactionData{
		Summary: summary, ReplacesUpToSeq: upTo,
		TokensBefore: estimateTokens("", msgs),
		TokensAfter:  estimateTokens(summary, nil),
	})
	return a.appendFact(ctx, c.SessionID, event.TypeCompaction, c.Actor, data)
}

// Subscribe replays persisted events from fromSeq, then streams live events,
// de-duplicating any fact events that appear in both (F-STORE-READ-REPLAY).
func (a *App) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	live, cancel := a.bus.Subscribe(ctx, sid)
	past, err := a.store.Read(ctx, sid, fromSeq)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	var maxSeq int64
	for _, e := range past {
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}

	out := make(chan event.Event)
	go func() {
		defer close(out)
		for _, e := range past {
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
		for e := range live {
			if e.Type.IsFact() && e.Seq != 0 && e.Seq <= maxSeq {
				continue // already replayed
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, cancel, nil
}

// ListSessions returns session metadata for a workdir.
func (a *App) ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error) {
	return a.store.ListSessions(ctx, workdir)
}

// ChildView is a finished subagent's restored transcript (for resuming a parent
// session's subagent panes).
type ChildView struct {
	ID       session.SessionID
	Role     string
	Messages []session.Message
}

// ChildSessions returns the subagent sessions spawned by parent, each with its
// reconstructed transcript, so a UI can restore them as (done) panes on resume.
func (a *App) ChildSessions(ctx context.Context, workdir string, parent session.SessionID) ([]ChildView, error) {
	metas, err := a.store.ChildSessions(ctx, workdir, string(parent))
	if err != nil {
		return nil, err
	}
	out := make([]ChildView, 0, len(metas))
	for _, m := range metas {
		msgs, _, err := a.SessionState(ctx, m.ID)
		if err != nil {
			continue
		}
		out = append(out, ChildView{ID: m.ID, Role: m.Agent, Messages: msgs})
	}
	return out, nil
}

// ---- internals ----

// appendFact persists a fact event (assigning seq) and publishes it on the bus.
func (a *App) appendFact(ctx context.Context, sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) error {
	ev := event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Stage: a.currentStage(sid), Data: data}
	seqs, err := a.store.Append(ctx, sid, ev)
	if err != nil {
		return err
	}
	ev.Seq = seqs[0]
	a.touch(sid)
	a.bus.Publish(ev)
	return nil
}

// appendPromptText appends a single-text-part PromptSubmitted event to a session — the shared
// shape behind every "inject a note into a conversation" site (subagent Q&A, subagent results,
// refine success/failure records, plan-council notes, planner findings). Callers that must
// outlive the current turn pass context.WithoutCancel(ctx); the error is returned for the few
// sites that care and ignored (`_ =`) by the fire-and-forget ones.
func (a *App) appendPromptText(ctx context.Context, sid session.SessionID, actor event.Actor, text string) error {
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	return a.appendFact(ctx, sid, event.TypePromptSubmitted, actor, pd)
}

// publishTransient publishes a bus-only event (not persisted).
func (a *App) publishTransient(sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) {
	a.touch(sid)
	a.bus.Publish(event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Stage: a.currentStage(sid), Data: data})
}

// Loop stages tag events with the macro phase they belong to (D15).
const (
	stagePlan     = "plan"
	stageExecute  = "execute"
	stageCouncil  = "council"
	stageFinalize = "finalize"
)

// setStage records the current loop stage for a session; subsequent events are
// tagged with it (Loop map / rewind grouping).
func (a *App) setStage(sid session.SessionID, stage string) {
	a.mu.Lock()
	a.stage[sid] = stage
	a.mu.Unlock()
}

// currentStage returns the session's current stage, defaulting to execute.
func (a *App) currentStage(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.stage[sid]; s != "" {
		return s
	}
	return stageExecute
}

// touch records activity for a session (used by the sidecar liveness check).
func (a *App) touch(sid session.SessionID) {
	a.lastActivity.Store(sid, time.Now())
}

// idleFor returns how long a session has had no event activity.
func (a *App) idleFor(sid session.SessionID) time.Duration {
	if v, ok := a.lastActivity.Load(sid); ok {
		return time.Since(v.(time.Time))
	}
	return 0
}

func (a *App) sessionInfo(ctx context.Context, sid session.SessionID) session.Session {
	a.mu.Lock()
	s, ok := a.sessions[sid]
	a.mu.Unlock()
	if ok {
		return s
	}
	// Rebuild from the log if not cached (e.g. resumed session).
	evs, _ := a.store.Read(ctx, sid, 0)
	for _, e := range evs {
		if e.Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			_ = json.Unmarshal(e.Data, &d)
			s = session.Session{ID: sid, Workdir: d.Workdir, Agent: d.Agent, Model: d.Model}
			a.mu.Lock()
			a.sessions[sid] = s
			a.mu.Unlock()
			break
		}
	}
	return s
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// summarize is a placeholder summary used by Compact in M1.
func summarize(msgs []session.Message) string {
	return fmt.Sprintf("[compacted %d earlier messages]", len(msgs))
}

// RegisterContextProvider adds a context provider for RAG-like context injection.
func (a *App) RegisterContextProvider(p port.ContextProvider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.contextProviders = append(a.contextProviders, p)
}

// contextBudget caps the characters of provider-injected context per turn so a
// chatty RAG source can't blow the window.
const contextBudget = 8000

// gatherContext queries every registered context provider for the current
// request and returns their chunks formatted for the system prompt (empty if
// none). Each provider is bounded by a short timeout so a slow or hung source
// degrades to "no extra context" instead of stalling the turn.
func (a *App) gatherContext(ctx context.Context, q port.ContextQuery) string {
	a.mu.Lock()
	providers := append([]port.ContextProvider(nil), a.contextProviders...)
	a.mu.Unlock()
	if len(providers) == 0 {
		return ""
	}

	var b strings.Builder
	for _, p := range providers {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		chunks, err := p.Provide(cctx, q)
		cancel()
		if err != nil {
			continue // a failing provider must not break the turn
		}
		for _, c := range chunks {
			text := strings.TrimSpace(c.Text)
			if text == "" {
				continue
			}
			if c.Source != "" {
				b.WriteString("## " + c.Source + "\n")
			}
			b.WriteString(text + "\n\n")
			if b.Len() >= contextBudget {
				return strings.TrimSpace(b.String()[:contextBudget])
			}
		}
	}
	return strings.TrimSpace(b.String())
}
