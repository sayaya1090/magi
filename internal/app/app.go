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

	mu     sync.Mutex
	wg     sync.WaitGroup // tracks run + dispatch goroutines for graceful Close
	closed bool           // set by Close: no new run/dispatch goroutines (no Add after Wait)

	sem        chan struct{} // concurrency limiter for subagents (D7)
	spawnCount atomic.Int64  // cumulative subagents spawned (runaway backstop)

	lastActivity sync.Map // session.SessionID -> time.Time (liveness for the sidecar health check)
	toolsRunning sync.Map // session.SessionID -> *atomic.Int64 (tools in flight; suppresses the stall watchdog)

	memMu         sync.Mutex
	memCache      map[string]string       // workdir -> durable AGENTS.md memory
	skillCache    map[string][]port.Skill // workdir -> loaded skills
	skillCacheSig map[string]string       // workdir -> source-dir mtime signature (invalidates skillCache)

	permPolicy string  // runtime-adjustable permission policy (guarded by mu)
	policy     *Policy // guardrail rules engine (deny floor, allow rules, bash scan)

	probingWindows map[string]struct{} // models whose context window is being (or was) lazily probed (guarded by mu)

	pendingUserLabel string // user label set before any session existed (SSO startup login); applied at CreateSession (guarded by mu)

	llmLat llmLatencies // recent LLM round-trip durations per model (elastic subagent cap input)

	states map[session.SessionID]*sessionState // per-session state, consolidating the maps above (guarded by mu); migrated group-by-group
}

// sessionState holds all per-session state for one session, consolidating what used to
// be ~18 separate map[session.SessionID]X fields on App. One entry is created lazily on
// first access (state/stateLocked) and lives for the process lifetime — a nil/zero field
// is the "absent" signal (e.g. cancel==nil means no in-flight run), matching the old
// "key absent" semantics. All fields are guarded by App.mu; child sessions get their own
// entry just like top-level ones. Turn-scoped fields are zeroed by resetForNewTopLevel;
// the rest live for the whole session.
type sessionState struct {
	// Whole-session lifetime.
	cancel           context.CancelFunc     // in-flight run's cancel (Interrupt); nil = not running
	meta             session.Session        // session metadata cache
	todos            []session.Todo         // per-session plan
	stage            string                 // current loop stage for event tagging
	lastPromptTokens int                    // real prompt_tokens from the last turn
	observedEvents   int                    // event high-water mark of the last turn_finished observation (stale-answer guard)
	pendingInterject []pendingInterjection  // interjections queued to run as their own turn
	turnControl      turnControl            // pending mid-turn routing/replan signal
	perms            map[string]chan string // pending permission decisions by call id
	questions        map[string]chan string // pending ask_user picks by call id
	grants           map[string]bool        // "always" grants per tool
	pendingAsk       chan string            // channel for a subagent's pending ask answer (parent)
	bg               *bgGroup               // background subagent tracking (parent)
	report           *subReport             // filed final report (subagent session)
	userLabel        string                 // display name for the user in the transcript (plugin set_user_label); "" = "you"
	// deferredAbandoned is the set of interjection origin MessageIDs that were queued in a
	// PRIOR process (F5 ledger) and never resolved — reconstructed once from the log on the
	// first run after load (deferredHydrated). Unlike pendingInterject (in-memory, lost on a
	// kill) these stay masked from the live turn context for the whole session so a stranded
	// interjection is not silently mixed into the next request; resetForNewTopLevel does NOT
	// clear them (the abandonment is a whole-session fact, not a per-turn one).
	deferredAbandoned map[string]bool
	deferredHydrated  bool
	// Turn-scoped (zeroed by resetForNewTopLevel).
	criteria        string          // elicited acceptance criteria this turn
	estSteps        int             // planner's advisory step estimate this turn
	interjectSeen   map[string]bool // interjection MessageIDs detected this turn (masked from turnTask/council)
	awaitExplorers  bool            // planner dispatched read-only explorers as this turn's primary work
	autoOrchestrate bool            // whether auto-orchestration has been triggered this session
}

// stateLocked returns the session's state, creating it on first use. The caller MUST
// hold a.mu. The nil-map guard lets zero-value App literals (used in tests) be safe;
// production always goes through New, which pre-allocates the map.
func (a *App) stateLocked(sid session.SessionID) *sessionState {
	if a.states == nil {
		a.states = map[session.SessionID]*sessionState{}
	}
	st := a.states[sid]
	if st == nil {
		st = &sessionState{}
		a.states[sid] = st
	}
	return st
}

// state returns the session's state, creating it on first use, taking a.mu itself.
func (a *App) state(sid session.SessionID) *sessionState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked(sid)
}

// stateIf returns the session's state without creating one — the read/liveness path that
// preserves the old "key absent" semantics (ok==false means nothing was ever recorded).
// The caller MUST hold a.mu.
func (a *App) stateIf(sid session.SessionID) (*sessionState, bool) {
	st, ok := a.states[sid]
	return st, ok
}

// metaLocked returns a session's cached metadata, preserving the old `a.sessions[sid]`
// present/absent semantics: ok is true only for a session that was actually created
// (its meta.ID is set), never for a state entry that a lazy per-session write created.
// The caller MUST hold a.mu.
func (a *App) metaLocked(sid session.SessionID) (session.Session, bool) {
	st, ok := a.stateIf(sid)
	if !ok || st.meta.ID == "" {
		return session.Session{}, false
	}
	return st.meta, true
}

// pendingInterjection is a mid-turn user message parked to run as its own turn once
// the current one ends. MsgID is the PromptSubmitted event that carried it: while the
// interjection sits in the queue, that event is masked from the LIVE-judgment views
// (the running turn's model context and the council's per-turn scan) so it can neither
// merge into the current turn nor reset the council's turn-boundary window. It becomes
// visible again the moment it leaves the queue (drained, or absorbed via route_interjection).
type pendingInterjection struct {
	MsgID string
	Text  string
}

// turnControl is a mid-turn control signal a tool records for the running loop to
// drain at its next step. The loop owns turnTask/councilTurn/guard (stack-local),
// so a tool cannot mutate them directly; it leaves this signal instead and the loop
// applies the reground. route routes a queued user interjection (queue|redirect|
// append); replan is the agent's own "this plan is unworkable" declaration.
type turnControl struct {
	route   string // "", "queue", "redirect", or "append"
	routeID string // the request id the route targets (route_interjection request_id); "" = oldest queued
	replan  bool
	reason  string
}

// New constructs an App.
func New(store port.Store, llm port.LLMProvider, tools port.ToolRegistry, b *bus.Bus, plat port.Platform, cfg Config) *App {
	c := cfg.withDefaults()
	c.ProfileModels = cloneStringMap(c.ProfileModels) // runtime edits must not mutate the caller's map
	return &App{
		store:          store,
		llm:            llm,
		providers:      cloneProviders(c.Providers),
		profileDefs:    cloneProfileDefs(c.ProfileDefs),
		routeOverrides: map[string]routeOverride{},
		tools:          tools,
		bus:            b,
		plat:           plat,
		cfg:            c,
		sem:            make(chan struct{}, c.Concurrency),
		permPolicy:     c.Permission,
		policy:         newPolicy(c.Allow, c.Deny, c.AllowDomains),
		probingWindows: map[string]struct{}{},
		states:         map[session.SessionID]*sessionState{},
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
	if a.stateLocked(sid).report != nil {
		return fmt.Errorf("you already filed a report this turn; your turn is ending")
	}
	a.stateLocked(sid).report = &subReport{summary: summary, status: status, details: details}
	return nil
}

// takeReport returns and clears any report filed for a session.
func (a *App) takeReport(sid session.SessionID) *subReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.stateLocked(sid).report
	a.stateLocked(sid).report = nil
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

// realPromptTokens returns the actual prompt token count from the last turn (0
// if not yet known).
func (a *App) realPromptTokens(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.lastPromptTokens
	}
	return 0
}

func (a *App) setPromptTokens(sid session.SessionID, n int) {
	a.mu.Lock()
	a.stateLocked(sid).lastPromptTokens = n
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

// shellCaptureCap bounds each of stdout/stderr captured for a `!`-inline-shell run
// (an OOM guard). It sits above the TUI's display/context trim so that trim, not
// this cap, decides the user-visible truncation note.
const shellCaptureCap = 256 << 10

// emptyTreeRef is git's well-known empty-tree object hash — a stable base to diff
// against in a repository that has no commits yet.
const emptyTreeRef = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

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
	a.stateLocked(sid).meta = s
	// A user label set before any session existed (an SSO plugin's startup login)
	// was latched — apply it so the identity rides every session from turn one.
	if a.pendingUserLabel != "" {
		a.stateLocked(sid).userLabel = a.pendingUserLabel
	}
	a.mu.Unlock()

	data, _ := json.Marshal(event.SessionCreatedData{Workdir: c.Workdir, Agent: c.Agent, Model: model})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, c.Actor, data); err != nil {
		return "", err
	}
	return sid, nil
}

// resetForNewTopLevel clears the per-task state that must not leak from a finished
// turn into a fresh top-level request: the plan/todos, cached acceptance criteria,
// and the advisory step estimate. Used by Submit and by the queued-interjection
// drain — a message that stayed queued runs as its OWN turn, so it must be judged on
// its own merits, not held to the previous task's contract (which made the council
// judge an unrelated queued request against the finished task's leftover plan).
func (a *App) resetForNewTopLevel(sid session.SessionID) {
	a.SetTodos(sid, nil) // takes a.mu itself
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.criteria = "" // drop cached criteria; re-elicited at the next gate (D15)
	st.estSteps = 0  // …and the previous task's advisory step estimate
	// Reset the interjection mask, but KEEP masking anything still WAITING in the
	// queue: a queued interjection's original PromptSubmitted must stay hidden
	// until it runs as its own turn — dropping its mask here would leak it into
	// the new turn's context and double-answer it.
	var keep map[string]bool
	for _, it := range st.pendingInterject {
		if keep == nil {
			keep = map[string]bool{}
		}
		keep[it.MsgID] = true
	}
	st.interjectSeen = keep
	st.awaitExplorers = false // the async-explorer wait is per-turn; the next turn starts clean
	a.mu.Unlock()
}

// setAwaitExplorers marks (or clears) that the planner dispatched read-only explorers as
// this turn's primary work, so the loop's pre-model park engages until they report.
func (a *App) setAwaitExplorers(sid session.SessionID, v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateLocked(sid).awaitExplorers = v
}

// awaitingExplorers reports whether this turn is parked waiting for planner-dispatched
// read-only explorers. Distinguishes the async-explorer scenario (park pre-model, no
// findings-less review) from ordinary background delegation (interleave own work).
func (a *App) awaitingExplorers(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.awaitExplorers
	}
	return false
}

// Submit appends the user's prompt and starts the agent loop asynchronously.
func (a *App) Submit(ctx context.Context, c command.SubmitPrompt) error {
	// A new top-level request starts with a fresh per-task contract so the
	// previous turn's plan/criteria don't leak into the new one (see
	// resetForNewTopLevel). The agent repopulates the plan via todowrite if
	// the new task warrants one.
	a.resetForNewTopLevel(c.SessionID)
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
	st, ok := a.stateIf(c.SessionID)
	running := ok && st.cancel != nil
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
	// A user request carries a time-sortable id (routing binds to it, and the display layer
	// pairs it with its response); system-injected prompts keep the cheap random id.
	msgID := "m_" + newID()
	if c.Actor.Kind == event.ActorUser {
		msgID = "m_" + newSortableID()
	}
	data, _ := json.Marshal(event.PromptSubmittedData{MessageID: msgID, Parts: c.Parts})
	if err := a.appendFact(ctx, c.SessionID, event.TypePromptSubmitted, c.Actor, data); err != nil {
		return err
	}
	// Observation: surface genuine user prompts (not system/council injections)
	// to observer plugins. The observer queues and returns — never blocks here.
	if a.cfg.Observer != nil && c.Actor.Kind == event.ActorUser {
		var texts []string
		for _, pt := range c.Parts {
			if pt.Kind == session.PartText && strings.TrimSpace(pt.Text) != "" {
				texts = append(texts, pt.Text)
			}
		}
		if len(texts) > 0 {
			a.cfg.Observer.UserMessage(string(c.SessionID), strings.Join(texts, "\n"))
		}
	}
	return nil
}

// appendResurfacedPrompt re-emits a queued interjection as a fresh user prompt that
// runs as its own turn (like appendPrompt), but links back to the original prompt's
// MessageID via ResurfacedFrom so the display layer can pair the query with its
// answer (drop the stranded original on replay; pull the live bubble down). Turn
// semantics are identical to appendPrompt — the link is display-only metadata.
func (a *App) appendResurfacedPrompt(ctx context.Context, sid session.SessionID, originMsgID, text string) error {
	a.setStage(sid, stageExecute)
	data, _ := json.Marshal(event.PromptSubmittedData{
		MessageID:      "m_" + newSortableID(),
		Parts:          []session.Part{{Kind: session.PartText, Text: text}},
		ResurfacedFrom: originMsgID,
	})
	return a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"}, data)
}

// taskEvents returns evs with EVERY interjection detected this turn removed, for the
// task-identity views — turnTask derivation and the council's per-turn evidence scan.
// Wider than liveEvents: it also hides interjections the orchestrator answered inline
// (never queued) so they cannot swap what the council judges against.
func (a *App) taskEvents(sid session.SessionID, evs []event.Event) []event.Event {
	return filterDeferredEvents(evs, a.interjectSeenIDs(sid))
}

// PluginNote appends a system note from a plugin to the session transcript —
// the plugin host's magi.notify. It uses the established system-actor prompt
// pattern (council/planner notes), so it renders as a ⟳ note, never counts as
// an unanswered user prompt, and the model sees it next turn (an "engram saved
// skill X — reply N to undo" notice is actionable precisely because the model
// and the user both see it).
func (a *App) PluginNote(sessionID, text string) {
	text = strings.TrimSpace(text)
	if sessionID == "" || text == "" {
		return
	}
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(context.Background(), session.SessionID(sessionID), event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "plugin"}, pd)
}

// observeTurnFinished surfaces a completed top-level turn to observer plugins:
// the final assistant text is what a lesson-extraction observer analyzes. Fired
// after run() returns whatever way the turn ended. Only assistant text NEWER
// than the last observation fires — a turn that produced no new answer (provider
// error, cancel before any text) must not re-fire the previous turn's answer as
// if it were this one's. The observer enqueues and returns.
func (a *App) observeTurnFinished(ctx context.Context, sid session.SessionID) {
	if a.cfg.Observer == nil {
		return
	}
	// Skip the store scan entirely when nothing listens (headless/bench with no
	// observer plugins) — this runs on every top-level turn.
	if w, ok := a.cfg.Observer.(interface{ WantsTurnFinished() bool }); ok && !w.WantsTurnFinished() {
		return
	}
	// A cancelled turn still carries whatever was said — don't let the dead
	// runCtx suppress the read.
	evs, err := a.store.Read(context.WithoutCancel(ctx), sid, 0)
	if err != nil {
		return
	}
	a.mu.Lock()
	seen := a.stateLocked(sid).observedEvents
	a.stateLocked(sid).observedEvents = len(evs)
	userLabel := a.stateLocked(sid).userLabel
	a.mu.Unlock()
	if seen > len(evs) {
		seen = 0 // defensive: store shrank (should not happen)
	}

	// Structural outcome from this turn's event window: the host KNOWS how the
	// turn ended, so observers get ground truth instead of parsing phrasing.
	// Precedence: unverified > verified > guard > error > done — an unverified
	// landing is authoritative over a council vote earlier in the turn, and a
	// council-approved finish outranks a transient error it recovered from.
	var finalText string
	var skillsLoaded []string
	skillSeen := map[string]bool{}
	sawVerified, sawUnverified, sawGuard, sawError := false, false, false, false
	sawToolCall, sawCouncil := false, false
	reasonUnverified, reasonGuard, reasonError := "", "", ""
	for _, e := range evs[seen:] {
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Role == session.RoleAssistant && d.Part.Kind == session.PartText {
				if t := strings.TrimSpace(d.Part.Text); t != "" {
					finalText = t
				}
			}
			if d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil {
				sawToolCall = true // this turn did real work — the council's own gate trigger (usedTools)
				// Skill loads this turn (usage metering for observers).
				if d.Part.ToolCall.Name == "skill" {
					var sa struct {
						Name string `json:"name"`
					}
					if json.Unmarshal(d.Part.ToolCall.Args, &sa) == nil && sa.Name != "" && !skillSeen[sa.Name] {
						skillSeen[sa.Name] = true
						skillsLoaded = append(skillsLoaded, sa.Name)
					}
				}
			}
		case event.TypeTurnFinished:
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil && d.Unverified {
				sawUnverified, reasonUnverified = true, d.Reason
			}
		case event.TypeCouncilDecided:
			sawCouncil = true // the consensus gate actually ran this turn (approved or forced)
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && d.Phase == "" && d.Decision == string(council.Done) && !d.Forced {
				sawVerified = true
			}
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Code == "loop_guard" || d.Code == "stall_guard" {
				sawGuard, reasonGuard = true, d.Code
			} else {
				sawError, reasonError = true, d.Message
			}
		}
	}
	if finalText == "" {
		return // no new assistant text this turn — nothing for an observer to analyze
	}
	outcome, reason := "done", ""
	switch {
	case sawUnverified:
		outcome, reason = "unverified", reasonUnverified
	case sawVerified:
		outcome = "verified"
	case sawGuard:
		outcome, reason = "guard", reasonGuard
	case sawError:
		outcome, reason = "error", reasonError
	case sawToolCall && !sawCouncil:
		// A turn that did real work (the council's own usedTools trigger) yet no
		// consensus gate ran — council disabled, workflow mode, or a sub-depth
		// finish. Surface it instead of silently labelling it "done" so observers
		// don't record an unconfirmed completion as a success.
		outcome, reason = "ungated", "no verification gate ran on a tool-using turn"
	}
	a.cfg.Observer.TurnFinished(string(sid), TurnObservation{
		FinalText: finalText, Outcome: outcome, Reason: reason,
		SkillsLoaded: skillsLoaded, UserLabel: userLabel,
	})
}

// startRun launches the agent loop for a session unless one is already running
// (single run goroutine per session). After the loop ends it re-checks, under
// the lock, for a user message that was steered in during the exit window and
// runs again so nothing is stranded.
func (a *App) startRun(ctx context.Context, sid session.SessionID) {
	// Before any turn processes this session's events in THIS process, reconstruct which
	// interjections a prior process left queued-but-unresolved (F5) so they stay masked from
	// the turn context instead of leaking in as pending prompts. One-shot per session.
	a.ensureDeferredHydrated(ctx, sid)
	a.mu.Lock()
	st := a.stateLocked(sid)
	if a.closed || st.cancel != nil {
		a.mu.Unlock()
		return // shutting down, or already running (the loop picks up steered input on re-read)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st.cancel = cancel
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		defer cancel()
		for {
			err := a.run(runCtx, sid)
			a.observeTurnFinished(runCtx, sid)
			// Do NOT re-run or triage after a terminal error (e.g. a provider 429/5xx) or a
			// cancel: the prompt is still "unanswered", and re-running would hammer a failing
			// backend into an error storm. The error event already ended the turn.
			alive := err == nil && runCtx.Err() == nil

			// (1) A steer that landed as the trailing message (Steer appends to the store) is seen
			// by hasUnansweredUserPrompt — re-run at once to answer it. Under the SAME lock, snapshot
			// the user-prompt high-water mark: at this instant no unanswered steer trails, so every
			// counted prompt is already answered or is a queued item's original. A steer arriving
			// later increments the count past baseInput and is caught at teardown (3) — even if a
			// triage reply later buries it (an ActorAgent part hides it from hasUnansweredUserPrompt's
			// last-message view AND makes seedPromptIdx treat it as answered).
			a.mu.Lock()
			if alive && a.hasUnansweredUserPrompt(runCtx, sid) {
				a.mu.Unlock()
				continue
			}
			baseInput := 0
			if alive {
				evs, _ := a.store.Read(runCtx, sid, 0)
				baseInput = len(userPromptEntries(evs))
			}
			a.mu.Unlock()

			// (2) Drain queued interjections one at a time with finish-boundary triage: a focused
			// mini-turn answers a question/chitchat inline (in the session's own recent context,
			// no fresh-slate reset) and moves on; anything needing real work escalates to its own
			// top-level turn. Pop under the lock (atomic vs. enqueue), then triage unlocked (it
			// runs the model). A steer arriving mid-triage is caught by the count re-check in (3).
			if alive {
				rerun := false
				for runCtx.Err() == nil {
					a.mu.Lock()
					q := a.stateLocked(sid).pendingInterject
					if len(q) == 0 {
						a.mu.Unlock()
						break
					}
					item := q[0]
					if len(q) == 1 {
						a.stateLocked(sid).pendingInterject = nil
					} else {
						a.stateLocked(sid).pendingInterject = q[1:]
					}
					a.mu.Unlock()
					text := strings.TrimSpace(item.Text)
					if text == "" {
						continue
					}
					s := a.sessionInfo(runCtx, sid)
					if a.triageQueued(runCtx, a.agentFor(s), s, item.MsgID, text) {
						// Escalate: run it as its OWN top-level turn with the same fresh slate Submit
						// gives, so the council judges it on its own merits instead of the finished
						// task's plan/criteria. Link back to the original prompt so the display layer
						// pairs the query with its answer. If the ctx was cancelled during triage,
						// persist it but do NOT re-run on a dead ctx (no-retry-storm).
						a.resetForNewTopLevel(sid)
						_ = a.appendResurfacedPrompt(context.WithoutCancel(runCtx), sid, item.MsgID, text)
						if runCtx.Err() == nil {
							rerun = true
						}
						break
					}
					// Answered inline — already popped and the reply persisted; drain the next.
					// Ledger it resolved (F5) so a later reload does not see the deferred entry
					// with no resolution and wrongly re-mask an interjection that was answered.
					a.recordDeferral(context.WithoutCancel(runCtx), sid, item.MsgID, true)
				}
				if rerun {
					continue
				}
			}

			// (3) Retire the run goroutine. Re-read the user-prompt high-water mark under the SAME
			// lock as the cancels delete, so a steer that landed during triage — even one a triage
			// reply buried (invisible to both hasUnansweredUserPrompt and seedPromptIdx) — is caught
			// rather than stranded. Steer takes a.mu for its running check, so it serializes: we
			// either see the new input here, or Steer restarts the retired goroutine.
			a.mu.Lock()
			var newSteers []userPrompt
			if alive {
				evs, _ := a.store.Read(runCtx, sid, 0)
				if np := userPromptEntries(evs); len(np) > baseInput {
					newSteers = np[baseInput:] // genuine steers that arrived after the snapshot
				}
			}
			// Only re-run while the ctx is live; on a cancel we still recover the input below
			// (persist-only) but must not re-run on a dead ctx (no-retry-storm).
			if alive && runCtx.Err() == nil && (len(newSteers) > 0 || len(a.stateLocked(sid).pendingInterject) > 0) {
				a.mu.Unlock()
				// Re-surface every prompt past the baseline as its own turn (fresh contract) so the
				// re-run seeds onto it even when a triage reply buried it in the transcript.
				if len(newSteers) > 0 {
					a.resetForNewTopLevel(sid)
					for _, p := range newSteers {
						if txt := strings.TrimSpace(p.Text); txt != "" {
							_ = a.appendResurfacedPrompt(context.WithoutCancel(runCtx), sid, p.MsgID, txt)
						}
					}
				}
				continue
			}
			a.stateLocked(sid).cancel = nil
			queued := a.stateLocked(sid).pendingInterject
			a.stateLocked(sid).pendingInterject = nil
			a.mu.Unlock()
			// Terminal error/cancel path: persist any still-queued interjection AND any steer that
			// arrived (possibly buried by a triage reply) during a now-cancelled drain, so both
			// survive to the next run instead of being silently lost — but do NOT re-run here
			// (no-retry-storm on a failing/cancelled backend). newSteers and queued are disjoint: a
			// queued item's original prompt predates the baseline, so it is never in newSteers.
			if len(newSteers) > 0 || len(queued) > 0 {
				// Clear the finished task's contract so they don't inherit it when picked up.
				a.resetForNewTopLevel(sid)
				for _, p := range newSteers {
					if txt := strings.TrimSpace(p.Text); txt != "" {
						_ = a.appendResurfacedPrompt(context.WithoutCancel(runCtx), sid, p.MsgID, txt)
					}
				}
				for _, q := range queued {
					if text := strings.TrimSpace(q.Text); text != "" {
						_ = a.appendResurfacedPrompt(context.WithoutCancel(runCtx), sid, q.MsgID, text)
					}
				}
			}
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
	for _, st := range a.states {
		if st.cancel != nil {
			st.cancel()
		}
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
	var cancel context.CancelFunc
	if st, ok := a.stateIf(c.SessionID); ok {
		cancel = st.cancel
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// RespondQuestion delivers the user's pick to a waiting ask_user execution.
func (a *App) RespondQuestion(ctx context.Context, c command.RespondQuestion) error {
	a.mu.Lock()
	var ch chan string
	if st, ok := a.stateIf(c.SessionID); ok {
		ch = st.questions[c.CallID]
	}
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
	var ch chan string
	if st, ok := a.stateIf(c.SessionID); ok {
		ch = st.perms[c.CallID]
	}
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

// publishTransient publishes a bus-only event (not persisted). No-op when the App
// was built without a bus (minimal test construction) — a transient event has no
// meaning with no subscribers.
func (a *App) publishTransient(sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) {
	if a.bus == nil {
		return
	}
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
	a.stateLocked(sid).stage = stage
	a.mu.Unlock()
}

// currentStage returns the session's current stage, defaulting to execute.
func (a *App) currentStage(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok && st.stage != "" {
		return st.stage
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

// enterTool / leaveTool bracket a single tool execution for a session, and
// toolInFlight reports whether any tool is currently running. The stall watchdog
// consults toolInFlight so a legitimately long, silent tool (e.g. a multi-minute
// bash build that emits no events until it returns) is not mistaken for a wedged
// child. A tool that hangs past its own timeout is still bounded by the hard cap.
func (a *App) enterTool(sid session.SessionID) {
	v, _ := a.toolsRunning.LoadOrStore(sid, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (a *App) leaveTool(sid session.SessionID) {
	if v, ok := a.toolsRunning.Load(sid); ok {
		v.(*atomic.Int64).Add(-1)
	}
}

func (a *App) toolInFlight(sid session.SessionID) bool {
	if v, ok := a.toolsRunning.Load(sid); ok {
		return v.(*atomic.Int64).Load() > 0
	}
	return false
}

func (a *App) sessionInfo(ctx context.Context, sid session.SessionID) session.Session {
	a.mu.Lock()
	s, ok := a.metaLocked(sid)
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
			a.stateLocked(sid).meta = s
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

// newSortableID returns a 32-hex-char identifier whose lexicographic order matches
// creation order: 6 bytes of big-endian Unix milliseconds followed by 10 crypto-random
// bytes (UUIDv7/ULID-style, dependency-free). Used for user-request MessageIDs so a request
// can be ordered, correlated with its response, and named back by the model when routing.
func newSortableID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	b[3], b[4], b[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	_, _ = rand.Read(b[6:])
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
