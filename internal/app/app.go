// Package app wires the ports together into the application service: it turns
// commands into persisted events and a live event stream (CQRS-lite, DESIGN §4).
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// AgentSpec defines a named agent: its system prompt, optional tool allowlist
// (empty = all tools), and optional model override. (F-AGENT-MULTI)
type AgentSpec struct {
	Name     string
	System   string
	Tools    []string
	Model    session.ModelRef
	Provider string // named LLM profile (different endpoint/key); empty = default backend
}

// AgentRoute is an agent's current effective backend routing, for display and
// interactive editing (the /route menu).
type AgentRoute struct {
	Name     string
	Model    string
	Provider string // named profile, or "" for the default backend
}

// routeOverride is a runtime per-agent routing edit applied over config.
type routeOverride struct{ model, provider string }

// ProfileDef is a named LLM backend definition (endpoint/key/model + one optional
// custom header), editable in the /route menu and persisted to [llm.profiles.*].
type ProfileDef struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
}

// ProviderFactory builds an LLM provider for a profile (injected by main so the
// app stays decoupled from the concrete LLM adapter).
type ProviderFactory func(ProfileDef) port.LLMProvider

// RoutePersister writes /route editor edits back to the config file so they
// persist across restarts. agent="" with a model persists the session default
// model; otherwise it persists [routing] agent = value (empty value clears).
type RoutePersister interface {
	PersistRoute(agent, value string) error
	PersistModel(modelID string) error
	PersistProfile(p ProfileDef) error
}

func (s AgentSpec) allows(tool string) bool {
	if len(s.Tools) == 0 {
		return true
	}
	for _, t := range s.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// Config holds run-time policy for the agent loop and orchestration (D7).
type Config struct {
	Model      session.ModelRef
	System     string
	MaxSteps   int
	Permission string // "allow" | "deny" | "ask" | "auto" (approval axis)
	// DangerTools require permission before execution (when Permission == "ask").
	DangerTools map[string]bool

	// Guardrail policy (sandbox × approval, two-axis inspired). Profile
	// is a posture preset (safe|standard|yolo) that fills the axes and rules
	// below when they're unset; explicit fields override the preset.
	Profile string // "safe" | "standard" | "yolo"
	Sandbox string // "read-only" | "workspace-write" | "full" (OS sandbox axis)
	// Allow/Deny are pattern rules "Tool(spec)" — e.g. Bash(git push:*),
	// Read(**/.env), WebFetch(domain:example.com). Deny is a hard floor; secret
	// paths are denied by default in addition to these.
	Allow []string
	Deny  []string
	// AllowDomains restricts WebFetch/bash network egress to these hosts (and
	// subdomains). Empty = no host allowlist.
	AllowDomains []string

	// Agents are named subagents spawnable via the task tool.
	Agents map[string]AgentSpec
	// Bounded recursion limits (D7).
	MaxDepth    int // max subagent nesting depth
	MaxAgents   int // cumulative spawn cap (runaway backstop)
	Concurrency int // max concurrently running subagents

	// Models supplies model metadata for cost, routing, and context-aware
	// compaction (M6). Defaulted if nil.
	Models *model.Registry
	// CompactRatio triggers auto-compaction when estimated context tokens exceed
	// ContextWindow * ratio. Defaulted to 0.8.
	CompactRatio float64

	// Experience is the shared team memory/skills store (D13). Optional.
	Experience port.ExperienceStore

	// Hooks are lifecycle automations (PreToolUse/PostToolUse/Stop) that enforce
	// team procedure. Harness enables built-in defaults (e.g. gofmt on save).
	Hooks   []HookSpec
	Harness bool

	// Workflow drives substantial tasks through a deterministic, code-enforced
	// phase pipeline (localize → implement → verify → review → summarize) instead
	// of one free-form turn. VerifyCmd is the authoritative verification command
	// (auto-detected when empty); WorkflowMaxLoops bounds implement↔verify retries.
	Workflow         bool
	VerifyCmd        string
	WorkflowMaxLoops int

	// Subagent supervision (sidecar): each background subagent is watched for
	// liveness and restarted on stall/timeout/transient error.
	SubagentTimeout     time.Duration // hard cap per attempt (default 5m)
	SubagentStall       time.Duration // no-activity → considered stalled (default 90s)
	SubagentMaxRestarts int           // restarts on stall/timeout/error (default 2)

	// AutoOrchestrate triggers automatic orchestration mode when context usage
	// exceeds the threshold. -1 = disabled (user can toggle off), 0 = use default (0.6),
	// 0.0 < x <= 1.0 = custom threshold (e.g., 0.7 = 70%).
	// When triggered, the system injects a directive forcing the agent to
	// decompose work and delegate to subagents.
	AutoOrchestrate float64 // default 0.6 (60%)

	// Providers are named LLM profiles (per-agent endpoint/key routing): an
	// AgentSpec.Provider names one of these to run on a different backend.
	Providers map[string]port.LLMProvider

	// ProfileModels maps a profile name to its model, so the /route menu can
	// switch an agent to a profile (setting both provider and model) at runtime.
	ProfileModels map[string]string

	// ProfileDefs holds the full profile definitions (endpoint/key/model/headers)
	// so the /route menu can list and edit them. NewProvider builds a provider for
	// a profile added/edited at runtime.
	ProfileDefs map[string]ProfileDef
	NewProvider ProviderFactory

	// RoutePersister, if set, persists /route editor edits to config so they
	// survive restarts (best-effort; a write failure never blocks the edit).
	RoutePersister RoutePersister

	// Planner enables the pre-flight planner: before a top-level turn it judges
	// whether the task splits into independent areas and, if so, fans out parallel
	// read-only explorers and injects their findings before the main agent runs.
	Planner bool

	// Council, when non-nil, gates loop termination at depth 0 (D14): when the
	// model would finish, a consensus council votes done/continue instead, and a
	// "continue" injects the members' aggregated feedback back into the loop. nil
	// disables the gate (the model's stop is final, the historical behavior).
	Council port.Council
	// CouncilRule is the consensus rule (default majority); CouncilMaxRounds caps
	// council rounds per turn (default 3); CouncilMembers overrides the default
	// MAGI trio.
	CouncilRule      council.Rule
	CouncilMaxRounds int
	CouncilMembers   []council.Member
}

// withDefaults fills unset fields with sensible values.
func (c Config) withDefaults() Config {
	if c.MaxSteps == 0 {
		c.MaxSteps = 40 // headroom for orchestration (delegate → incorporate → follow-up)
	}
	c = c.applyProfile()
	if c.Permission == "" {
		c.Permission = "ask"
	}
	if c.DangerTools == nil {
		c.DangerTools = map[string]bool{"write": true, "edit": true, "multiedit": true, "bash": true, "webfetch": true}
	}
	if c.MaxDepth == 0 {
		c.MaxDepth = 3
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = 50
	}
	if c.Concurrency == 0 {
		c.Concurrency = 8
	}
	if c.Models == nil {
		c.Models = model.NewRegistry()
	}
	if c.CompactRatio == 0 {
		c.CompactRatio = 0.8
	}
	if c.SubagentTimeout == 0 {
		c.SubagentTimeout = 5 * time.Minute
	}
	if c.SubagentStall == 0 {
		// Generous: time-to-first-token on a large prompt (e.g. a big doc review)
		// can be long with no stream activity; too tight a stall causes false
		// restarts (duplicate subagent panes). The hard timeout is the real cap.
		c.SubagentStall = 4 * time.Minute
	}
	if c.SubagentMaxRestarts == 0 {
		c.SubagentMaxRestarts = 2
	}
	if c.WorkflowMaxLoops == 0 {
		c.WorkflowMaxLoops = 3
	}
	// AutoOrchestrate: 0 = use default, -1 = explicitly disabled
	if c.AutoOrchestrate == 0 {
		c.AutoOrchestrate = 0.6 // 60% context usage triggers auto-orchestration
	}
	return c
}

// applyProfile fills the sandbox/approval axes from the posture preset when they
// are unset. Explicit fields always win, so a profile is just a convenient
// default bundle (OS-level sandbox × approval, surfaced as safe/standard/yolo).
func (c Config) applyProfile() Config {
	switch c.Profile {
	case "safe":
		if c.Permission == "" {
			c.Permission = "ask"
		}
		if c.Sandbox == "" {
			c.Sandbox = "read-only"
		}
	case "yolo":
		if c.Permission == "" {
			c.Permission = "allow"
		}
		if c.Sandbox == "" {
			c.Sandbox = "full"
		}
	case "standard":
		// The recommended posture: auto-approve edits, prompt for commands and
		// network, confine writes to the workspace.
		if c.Permission == "" {
			c.Permission = "auto"
		}
		if c.Sandbox == "" {
			c.Sandbox = "workspace-write"
		}
	default:
		// No profile set: keep historical behavior — permission filled below, and
		// OS sandbox stays opt-in (empty Sandbox = unconfined) to avoid silently
		// cutting network/out-of-tree writes for existing users. The policy layer's
		// command scan + permission prompt still guard bash either way.
	}
	return c
}

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

	mu       sync.Mutex
	cancels  map[session.SessionID]context.CancelFunc     // in-flight runs (Interrupt)
	perms    map[session.SessionID]map[string]chan string // pending permission decisions
	grants   map[session.SessionID]map[string]bool        // "always" grants per tool
	sessions map[session.SessionID]session.Session        // session metadata cache

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
	autoOrchestrateActive map[session.SessionID]bool           // whether auto-orchestration has been triggered for this session (guarded by mu)
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
		grants:                map[session.SessionID]map[string]bool{},
		sessions:              map[session.SessionID]session.Session{},
		sem:                   make(chan struct{}, c.Concurrency),
		permPolicy:            c.Permission,
		policy:                newPolicy(c.Allow, c.Deny, c.AllowDomains),
		lastPromptTokens:      map[session.SessionID]int{},
		todos:                 map[session.SessionID][]session.Todo{},
		bg:                    map[session.SessionID]*bgGroup{},
		pendingAsks:           map[session.SessionID]chan string{},
		reports:               map[session.SessionID]*subReport{},
		autoOrchestrateActive: map[session.SessionID]bool{},
	}
}

// subReport is a subagent's filed final result (the explicit output contract).
type subReport struct {
	summary, status, details string
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
	out := "STATUS: " + strings.ToUpper(r.status) + "\n" + strings.TrimSpace(answer)
	if d := strings.TrimSpace(r.details); d != "" && !strings.Contains(answer, d) {
		out += "\n\n" + d
	}
	return out
}

// SetTodos replaces a session's plan (TodoWrite).
func (a *App) SetTodos(sid session.SessionID, td []session.Todo) {
	a.mu.Lock()
	a.todos[sid] = td
	a.mu.Unlock()
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

// GitDiff returns the working-tree diff for workdir (empty if no changes). It
// falls back to `git status --short` when the diff is empty but untracked files
// exist.
func (a *App) GitDiff(ctx context.Context, workdir string) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}
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
	if a.cancels[sid] != nil {
		a.mu.Unlock()
		return // already running; the loop will pick up steered input on re-read
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancels[sid] = cancel
	a.mu.Unlock()

	go func() {
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
	data, _ := json.Marshal(event.CompactionData{Summary: summary, ReplacesUpToSeq: upTo})
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
	ev := event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Data: data}
	seqs, err := a.store.Append(ctx, sid, ev)
	if err != nil {
		return err
	}
	ev.Seq = seqs[0]
	a.touch(sid)
	a.bus.Publish(ev)
	return nil
}

// publishTransient publishes a bus-only event (not persisted).
func (a *App) publishTransient(sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) {
	a.touch(sid)
	a.bus.Publish(event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Data: data})
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
