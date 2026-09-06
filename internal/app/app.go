// Package app wires the ports together into the application service: it turns
// commands into persisted events and a live event stream (CQRS-lite, DESIGN §4).
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	// subJobs is the register the pane strip polls for running children — see subagent_jobs.go.
	subJobs subagentJobs
	// journals records, per CHILD session, the first state magi saw of every path that child
	// touched, so a loop can put a failed round back — see restore.go.
	journals map[session.SessionID]*restoreJournal

	store       port.Store
	rawStore    port.Store // the store without the born-flush wrapper; only bear writes through it
	llm         port.LLMProvider
	providers   map[string]port.LLMProvider // named LLM profiles (per-agent endpoint/key routing)
	profileDefs map[string]ProfileDef       // profile definitions (guarded by mu), for the /route editor
	tools       port.ToolRegistry
	// toolServers attaches a tool server while this daemon runs. Set by the binary that owns the
	// MCP manager (core holds the port, never the adapter); nil in a build that has none, and the
	// door then refuses rather than pretending.
	toolServers port.ToolServers
	// subagentPrefs is the user's own settings per subagent (guarded by mu). Only what they
	// touched; the rest falls back to what the tool declared. Off means not advertised.
	subagentPrefs    map[string]SubagentPref
	bus              *bus.Bus
	plat             port.Platform
	cfg              Config
	contextProviders []port.ContextProvider // RAG-like context injectors

	// openFiles is the file each session's console editor currently has open: the unsaved buffer,
	// pushed from the web editor as the person types and injected into the next turn's volatile
	// context (see volatileContext). Ephemeral and never persisted — LookOver's philosophy, that a
	// half-typed buffer does not belong in the event log. Guarded by mu.
	openFiles map[session.SessionID]openFile

	// promptMu guards promptIndex, the per-workdir cache of past sessions' user prompts that composer
	// suggestion learns this person's phrasing from. Its own mutex, not mu: it is read on a pause in
	// typing and must never contend with a running turn. See suggest.go.
	promptMu    sync.Mutex
	promptIndex map[string]*workdirPrompts

	mu     sync.Mutex
	wg     sync.WaitGroup // tracks run + dispatch goroutines for graceful Close
	closed bool           // set by Close: no new run/dispatch goroutines (no Add after Wait)
	// bg is the lifetime of work that must outlive the turn that started it, and bgStop ends it.
	// See bgContext.
	bg     context.Context
	bgStop context.CancelFunc

	liveness sync.Map // session.SessionID -> *sessionLiveness (write-only record of a running session; see liveness.go)
	// meetingRounds counts MeetingSayIn turns in flight. They deliberately never enter the run
	// states (WritingRun's handover-queue semantics depend on that), which made them invisible to
	// Running() — and the auto-update idle gate, built on Running(), restarted daemons mid-meeting:
	// the preparation turn held it off (spawnChild registers a cancel) and the discussion rounds,
	// the long part, did not. This counter is the meeting's own "activity" fact, read via
	// MeetingActive by anything that must not fire while a round is being composed.
	meetingRounds atomic.Int64

	memMu         sync.Mutex
	memCache      map[string]memoryOf     // workdir -> durable AGENTS.md memory, with the file state it was read from
	skillCache    map[string][]port.Skill // workdir -> loaded skills
	skillCacheSig map[string]string       // workdir -> source-dir mtime signature (invalidates skillCache)

	searchMu    sync.Mutex
	searchCache map[session.SessionID]cachedTurns // session -> its turns, keyed by last activity

	// cronReload wakes the scheduler when the job definitions change. Non-nil only while a daemon
	// is running one. Guarded by mu.
	cronReload chan struct{}

	permPolicy string  // runtime-adjustable permission policy (guarded by mu)
	policy     *Policy // guardrail rules engine (deny floor, allow rules, bash scan)

	// probingWindows says, for each model whose window will not be probed again, WHY — because a
	// probe is in flight or already failed, because a backend answered, or because a person pinned
	// it. It was a bare set, one slot for three reasons, and a redirect could not tell a number a
	// backend gave us from one a person did. Guarded by mu; windowBase is where requests went when
	// these marks were taken.
	probingWindows map[string]windowMark
	windowBase     string

	pendingUserLabel string // user label set before any session existed (SSO startup login); applied at CreateSession (guarded by mu)

	// Token ledger (usage_meter.go): every request the metered provider serves, whether it came
	// from the agent's own stream, a council poll, or any side call. Its own mutex — recordUsage
	// runs on every model goroutine and must never queue behind the state lock.
	usage usageLedger

	states map[session.SessionID]*sessionState // per-session state, consolidating the maps above (guarded by mu); migrated group-by-group
}

// New constructs an App.
func New(store port.Store, llm port.LLMProvider, tools port.ToolRegistry, b *bus.Bus, plat port.Platform, cfg Config) *App {
	c := cfg.withDefaults()
	c.ProfileModels = cloneStringMap(c.ProfileModels) // runtime edits must not mutate the caller's map
	bg, stop := context.WithCancel(context.Background())
	a := &App{
		store:          store,
		rawStore:       store,
		llm:            llm,
		providers:      cloneProviders(c.Providers),
		profileDefs:    cloneProfileDefs(c.ProfileDefs),
		tools:          tools,
		subagentPrefs:  clonePrefs(c.SubagentPrefs),
		bus:            b,
		plat:           plat,
		cfg:            c,
		permPolicy:     c.Permission,
		policy:         newPolicy(c.Allow, c.Deny, c.AllowDomains),
		probingWindows: map[string]windowMark{},
		states:         map[session.SessionID]*sessionState{},
		bg:             bg,
		bgStop:         stop,
	}
	// Every other path writes through a store that flushes a held session.created first. See
	// bornStore: the rule belongs to the seam, not to each caller that remembers it.
	//
	// Only when there IS one. Wrapping unconditionally left a.store holding a non-nil struct around
	// a nil Store, which quietly disarmed every `a.store == nil` guard in the package — SetModel's
	// and recordObserved's both, each written so a store-less App announces instead of dying. They
	// could not fire, and the App died anyway one frame later, on the nil rawStore inside bear.
	// A guard that cannot be reached is worse than none: it is why nobody looked again.
	if store != nil {
		a.store = bornStore{Store: store, app: a}
	}
	// The floors ask the CALL what file it opens, and a tool can answer for itself now (see
	// port.FileTool). Set after the App exists because the answer goes through its registry.
	a.policy.touches = a.touchesFile
	return a
}

// bgContext is the lifetime of work that must outlive the turn that started it.
//
// The one thing it is not is a session's context. A watch on a companion's answer exists precisely
// because the tool call returned — tied to that call it would be cancelled at the moment it became
// useful, and tied to the turn it would be cancelled when the turn it was going to feed ends.
//
// Background for a zero-value App: the test literals in this package build one directly, and a nil
// context is a panic in a place that has nothing to do with what is being tested.
func (a *App) bgContext() context.Context {
	if a.bg == nil {
		return context.Background()
	}
	return a.bg
}

// UseToolServers gives this app the door to attach tool servers at runtime. Called once at wiring
// time by the binary that built the MCP manager.
func (a *App) UseToolServers(s port.ToolServers) { a.toolServers = s }

// AttachToolServer connects an HTTP MCP server to this companion and answers with the tools it
// brought. The names matter to the caller: they are what it may ask for now.
// owner names the conversation the tools belong to; empty is the whole daemon (port.ToolServers).
func (a *App) AttachToolServer(ctx context.Context, owner, name, url string, headers map[string]string) ([]string, error) {
	if a.toolServers == nil {
		return nil, fmt.Errorf("this build attaches no tool servers")
	}
	// A server may not take a name the companion already answers to. What this guard can catch is a
	// server called `read` or `bash`: MCP tools are namespaced (mcp__<server>__<tool>) so one cannot
	// shadow a builtin by advertising the name, but nothing stopped the SERVER from being called
	// that and reading oddly in every list.
	//
	// The namespace collision — two server names that become one prefix — is deliberately NOT
	// checked here. It is decided on the sanitised name, and sanitising belongs to the adapter that
	// does the naming; core computing its own version of that word is how the two came to disagree
	// in the first place. The adapter refuses it, with both names in the message.
	for _, t := range a.tools.List() {
		if t.Name() == name {
			return nil, fmt.Errorf("%q is the name of a tool this companion already has", name)
		}
	}
	return a.toolServers.Attach(ctx, owner, name, url, headers)
}

// DetachToolServer removes one by name: false when there was none, an error when there was one and
// this caller may not remove it.
func (a *App) DetachToolServer(owner, name string) (bool, error) {
	if a.toolServers == nil {
		return false, fmt.Errorf("this build attaches no tool servers")
	}
	return a.toolServers.Detach(owner, name)
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

// CreateSession opens a session and returns its id. The session.created fact is written when
// the session first has something in it — see sessionState.born.
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
	if who, fromAgent := HandoffOrigin(c.Actor.ID); fromAgent {
		a.stateLocked(sid).handedFrom = firstLine(who, 80)
	}
	// A user label set before any session existed (an SSO plugin's startup login)
	// was latched — apply it so the identity rides every session from turn one.
	if a.pendingUserLabel != "" {
		a.stateLocked(sid).userLabel = a.pendingUserLabel
	}
	a.mu.Unlock()

	// Held, not written. See sessionState.born: the id is real from here on and every consumer
	// that needs one has it, while a session nobody speaks in leaves nothing behind.
	data, _ := json.Marshal(event.SessionCreatedData{Workdir: c.Workdir, Agent: c.Agent, Model: model,
		Parent: c.Parent, Project: c.Project, For: c.For})
	a.mu.Lock()
	a.stateLocked(sid).born = &bornFact{actor: c.Actor, data: data, at: s.Created}
	a.mu.Unlock()
	return sid, nil
}

// Submit appends the user's prompt and starts the agent loop asynchronously.
func (a *App) Submit(ctx context.Context, c command.SubmitPrompt) error {
	// A new top-level request starts with a fresh per-task contract so the
	// previous turn's plan/criteria don't leak into the new one (see
	// resetForNewTopLevel). The agent repopulates the plan via todowrite if
	// the new task warrants one.
	a.appendRefs(ctx, &c)
	a.resetForNewTopLevel(c.SessionID)
	// Who asked decides whether anybody is waiting for an answer. A scheduled firing carries a
	// cron actor id, and that is the whole of "unattended" today — read here, at the one place a
	// top-level turn begins, rather than guessed at later from the session's contents.
	a.mu.Lock()
	a.stateLocked(c.SessionID).unattended = isUnattended(c.Actor)
	a.mu.Unlock()
	if err := a.appendPrompt(ctx, c); err != nil {
		return err
	}
	a.startRun(ctx, c.SessionID)
	return nil
}

// isUnattended reports whether a prompt came from something rather than somebody.
//
// One predicate, so the answer does not depend on which caller is asking. Today that is a cron
// firing; anything else that runs work without a person in front of it belongs here too, and the
// place to add it is this function rather than each waiting site.
func isUnattended(actor event.Actor) bool {
	_, isCron := CronOriginName(actor.ID)
	return isCron
}

// Steer injects a user message into a session that is already running, so the
// in-flight agent picks it up at its next step (it re-reads the conversation
// each step) instead of the user having to wait for the turn to finish. If no
// turn is running, it behaves like Submit and starts one.
func (a *App) Steer(ctx context.Context, c command.SubmitPrompt) error {
	a.appendRefs(ctx, &c)
	if err := a.appendPrompt(ctx, c); err != nil {
		return err
	}
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
			// The rendered attachment block stays out of the observation: it is workspace bytes,
			// not the person's words, and a plugin that wants file contents holds a file grant
			// rather than reading them off a user-message side channel (hunted 2026-08-29).
			if strings.HasPrefix(pt.Text, attachedHeader) {
				continue
			}
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
			sawCouncil = true // the consensus gate actually ran this turn
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && d.Decision == string(council.Done) {
				sawVerified = true
			}
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Recovered {
				continue // the run kept working past it; the turn's outcome is decided later
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
	outcome, reason := OutcomeDone, ""
	switch {
	case sawUnverified:
		outcome, reason = OutcomeUnverified, reasonUnverified
	case sawVerified:
		outcome = OutcomeVerified
	case sawGuard:
		outcome, reason = OutcomeGuard, reasonGuard
	case sawError:
		outcome, reason = OutcomeError, reasonError
	case sawToolCall && !sawCouncil:
		// A turn that did real work (the council's own usedTools trigger) yet no
		// consensus gate ran — council disabled, workflow mode, or a sub-depth
		// finish. Surface it instead of silently labelling it "done" so observers
		// don't record an unconfirmed completion as a success.
		outcome, reason = OutcomeUngated, "no verification gate ran on a tool-using turn"
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
		// lastErr is how the most recent turn ENDED, which is what decides whether the teardown
		// below owes a terminal event. A cancelled context is not that answer: see the emit.
		var lastErr error
		for {
			// The one instant the tool set may change. Nothing is stepping, so nothing can call a
			// tool that goes away underneath it — which is the objection that kept the companion
			// attachments frozen at startup. See Config.BetweenTurns.
			if a.cfg.BetweenTurns != nil {
				a.cfg.BetweenTurns(runCtx)
			}
			err := a.run(runCtx, sid)
			lastErr = err
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
			// Snapshot the deferred (queued-interjection) set BEFORE taking a.mu: the
			// re-run gate below runs under a.mu, and deferredInterjectIDs locks a.mu too —
			// calling it there would re-enter and deadlock. A queued item can only leave
			// the set (drained), never join it, while this goroutine sits between turns,
			// so a pre-lock snapshot is safe.
			deferredSnap := a.deferredInterjectIDs(sid)
			a.mu.Lock()
			if alive && a.hasUnansweredUserPrompt(runCtx, sid, deferredSnap) {
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
					a.mu.Unlock()
					// Settle the "answered" claims before the batch is coalesced. The claim says the
					// turn's reply already covers the request; the fact magi can check is whether the
					// turn said ANYTHING after the claim was made. Something said is not proof it
					// answers the question — that is the model's call and magi does not second-guess
					// it — but nothing said is proof it does not, and that is the case that matters,
					// because the alternative is a user request dropped on an assertion. A verified
					// claim leaves the queue here; an empty one loses its claim and is triaged like
					// any other queued item.
					q = a.settleAnsweredClaims(context.WithoutCancel(runCtx), sid)
					if len(q) == 0 {
						break
					}
					a.mu.Lock()
					// Take the whole batch off the queue, then decide each message on its OWN.
					//
					// It used to be merged into one interjection and triaged once, which is right for
					// the impatient case it was built for (the same thing typed three ways) and wrong
					// for every other: a batch holding one piece of work escalated as a unit, so the
					// questions in it were never answered — they rode along inside a work prompt.
					// The exact-duplicate half of that merge is kept below; the rest is per message.
					a.stateLocked(sid).pendingInterject = nil
					a.mu.Unlock()
					bctx := context.WithoutCancel(runCtx)
					q = a.dedupeInterjects(bctx, sid, q)
					// Off: fold the batch into its most recent member first, so the loop below runs
					// over one item and decides once — the previous behaviour, without a second copy
					// of the drain to keep in step with this one.
					if !interjectSplitEnabled() && len(q) > 1 {
						carrier := q[len(q)-1]
						merged := coalesceInterjectionText(q)
						for _, p := range q[:len(q)-1] {
							a.dropQueued(bctx, sid, p.MsgID)
						}
						q = []pendingInterjection{{MsgID: carrier.MsgID, Text: merged}}
					}

					// STRICTLY IN ORDER: a message is disposed of completely before the next is
					// looked at. Answering the whole batch first and escalating afterwards read
					// them in order but ANSWERED them out of it — a question typed second came
					// back before the work typed first, because the inline reply lands during the
					// pass and the escalated turn only starts after it. Reported exactly that way.
					//
					// So the first item that needs work ends the pass. It runs as its own turn and
					// everything behind it stays queued for the next boundary, which is what keeps
					// the answers in the order they were typed. The cost is that two pieces of work
					// are two turns; ordering is worth more than the turn.
					var work *pendingInterjection
					s := a.sessionInfo(runCtx, sid)
					for i := range q {
						// The ctx died mid-batch: put back everything not yet looked at, ahead of
						// anything that arrived meanwhile, rather than deciding it on a dead ctx.
						if runCtx.Err() != nil {
							a.requeueInterjects(sid, q[i:])
							break
						}
						if a.triageQueued(runCtx, a.agentFor(s), s, q[i].MsgID, q[i].Text) {
							work = &q[i]
							a.requeueInterjects(sid, q[i+1:]) // they wait their turn, in order
							break
						}
						// Answered inline, its reply already persisted and tagged InReplyTo. Ledger
						// it resolved (F5) so a reload does not read the entry as still waiting.
						a.recordDeferral(bctx, sid, q[i].MsgID, true)
						// The ledger is for the NEXT process. The screen in front of the person now
						// learns it from the transcript or not at all — and without this line it
						// did not, so the bubble stayed hoisted to the tail with its waiting glyph
						// while the answer to it was already on screen above.
						a.sayAnsweredInline(bctx, sid, q[i].MsgID)
					}

					if work != nil {
						// Escalate: its OWN top-level turn with the fresh slate Submit gives, so the
						// council judges it on its own merits and not the finished task's criteria.
						// Link back to the original prompt so the display layer pairs query and
						// answer. On a dead ctx, persist but do NOT re-run (no-retry-storm).
						a.resetForNewTopLevel(sid)
						_ = a.appendResurfacedPrompt(bctx, sid, work.MsgID, work.Text)
						if runCtx.Err() == nil {
							rerun = true
						}
						break
					}
					// Nothing needed a turn of its own. Loop to pick up whatever arrived while the
					// batch was being triaged.
				}
				// Whatever is still queued has now had a boundary pass over it: mark it, so the
				// next turn's start re-decides exactly these and nothing else.
				a.markBoundarySeen(sid)
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
		// On interruption the loop returns without a terminal event; emit one (on a detached
		// context, since runCtx is cancelled) so observers like the TUI stop showing "working"
		// instead of hanging forever.
		//
		// The condition is how the RUN ended, not whether the context is alive. It used to ask
		// runCtx.Err(), and a context cancelled after a normal finish — which is what a headless
		// one-shot does on its way out — made this fire on top of the terminal event the loop had
		// just written. Measured: every recorded session carries two turn.finished, the second with
		// `{"in":0,"out":0}`, so anything reading the LAST one (the fork boundary, the meter) reads
		// a turn that spent nothing. A run that ended any other way already ended it: a clean finish
		// writes turn.finished itself, and a provider error writes an error event.
		//
		// A provider error is the OTHER end that ends without a terminal turn.finished: the loop
		// writes a TypeError and returns, no finish. That is invisible to a handed-over piece's
		// asker — handoffAnswer keys on a PERSISTED turn.finished, so a run killed by a 429/5xx left
		// the asker told "still thinking" forever, its receipt orphaned. So a cancel AND a provider
		// error both get the terminal event here; only the cancel also abandons its seed prompt (the
		// work was abandoned), where the error leaves it as-is (the work failed). A clean finish
		// (lastErr == nil) wrote its own and must not get a second — the double this block once
		// produced is what the runCtx.Err() note above is about.
		writeFinish := false
		switch {
		case errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded):
			// Mark the cancelled turn's seed prompt abandoned so an unrelated next request
			// isn't anchored onto it (and a follow-up that augments it still seeds on itself).
			a.abandonSeedOnCancel(context.WithoutCancel(runCtx), sid)
			writeFinish = true
		case lastErr != nil:
			writeFinish = true
		}
		if writeFinish {
			d, _ := json.Marshal(event.TurnFinishedData{})
			_ = a.appendFact(context.WithoutCancel(runCtx), sid, event.TypeTurnFinished,
				event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
		}
		// A request made AFTER the person pressed stop still has to run.
		//
		// Two things had to be true for it to be lost, and both were: the sweep cleared it as if it
		// were part of the queue stop resets (fixed at the press — see cancelSweep), and nothing
		// started a turn for it. Steer had looked a moment earlier, seen a run still tearing down,
		// and left the prompt to it; that run was on its way out. Measured on a live companion:
		// stop, then "reply with exactly: pong", and the log reads prompt → abandoned → a note
		// promising "your newest request runs next" → turn.finished, with nothing ever answering.
		//
		// The trigger is the PRESS, not how the turn happened to die. A cancel reaches this code
		// two ways — as context.Canceled, or wrapped by the provider as an error carrying
		// "context canceled" — and only the first ran the sweep. Both leave the same person
		// waiting. cancelSweep is set by Interrupt and by nothing else, so it is exactly "somebody
		// stopped this turn"; a deadline, a shutdown or a 429 leaves it nil and nothing restarts
		// here (that is the retry storm the teardown above is careful about).
		a.mu.Lock()
		pressed := a.stateLocked(sid).cancelSweep
		a.stateLocked(sid).cancelSweep = nil
		shuttingDown := a.closed
		a.mu.Unlock()
		if pressed != nil && !shuttingDown {
			bctx := context.WithoutCancel(runCtx)
			if evs, rerr := a.store.Read(bctx, sid, 0); rerr == nil {
				// Anything the person said after the press: it cannot be part of what the press
				// cleared, and the turn that would have read it is the one they stopped.
				// The SAME question the sweep asked, or the comparison is meaningless: the
				// sweep holds the prompts that were OPEN at the press, while this used to list
				// every non-abandoned user prompt — answered ones included. In any session
				// with earlier answered requests (which is every session past its first turn)
				// one of them was always absent from the sweep, so "fresh" was always true and
				// every stop restarted the run. Measured on a live companion (2026-08-29):
				// two interrupts, and both times a turn.finished was followed seconds later by
				// new tool calls and a new approval prompt — stop that could not stop.
				fresh := false
				for _, id := range unansweredUserPromptIDs(evs) {
					if !pressed[id] {
						fresh = true
						break
					}
				}
				if fresh {
					a.resetForNewTopLevel(sid)
					a.startRun(bctx, sid)
				}
			}
		}
		// The run goroutine is retiring, so nothing is running — say so, whatever ended it.
		//
		// The persisted event above covers a cancelled run, and runLoop writes its own on a clean
		// finish. Neither covers the path this exists for: the turn ends and is recorded, and THEN
		// the drain answers a queued interjection inline. Observed live — turn.finished at 21:07:29,
		// the inline reply persisted at 21:07:41, and the spinner still turning eighteen minutes
		// later. The transcript is right to revive it (real tokens arrived after the turn ended, and
		// treating that as idle is the worse failure); what was missing is anything to turn it off.
		//
		// TRANSIENT, because a SECOND turn.finished in the log is a known defect in its own right:
		// every session carried two, the last with {"in":0,"out":0}, so the fork boundary and the
		// usage meter — which read the last one — read a turn that spent nothing. This reaches the
		// bus, where the display lives, and no reader of the store.
		td, _ := json.Marshal(event.TurnFinishedData{})
		a.publishTransient(sid, event.TypeTurnFinished, event.Actor{Kind: event.ActorSystem, ID: "loop"}, td)
	}()
}

// hasUnansweredUserPrompt reports whether the last message in the session is a
// user prompt with no assistant response after it (a steered-in message the
// agent has not yet handled).
func (a *App) hasUnansweredUserPrompt(ctx context.Context, sid session.SessionID, deferred map[string]bool) bool {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return false
	}
	// "Trailing user message" is too narrow: a prompt that arrives DURING the council's
	// finish deliberation is buried when the loop then appends the approved answer (and
	// council-decided facts), so the last reconstructed message is the assistant's — and
	// the new request is silently stranded. Ask the real question instead: is any genuine
	// user prompt still UNANSWERED, not abandoned, and NOT a queued interjection? The
	// deferred exclusion matters: a queued interjection is owned by the dedicated drain
	// path, so counting it here re-runs it in a loop (the recall bug); a mid-council
	// prompt is not queued (it arrived after the last step's scan), so it still re-runs.
	return hasUnansweredPrompt(evs, deferred)
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
	stop := a.bgStop
	a.mu.Unlock()
	if stop != nil {
		stop() // and the watches that deliberately outlive a turn
	}
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
	// What the cancel is allowed to clear is decided HERE, not when the turn gets round to
	// stopping: the sweep runs during teardown, and a request typed between the press and the
	// teardown is the person's next intent, not part of the queue they just reset.
	sweep := map[string]bool{}
	if evs, err := a.store.Read(ctx, c.SessionID, 0); err == nil {
		for _, id := range unansweredUserPromptIDs(evs) {
			sweep[id] = true
		}
	}
	a.mu.Lock()
	var cancel context.CancelFunc
	if st, ok := a.stateIf(c.SessionID); ok {
		cancel = st.cancel
		st.cancelSweep = sweep
		// Anything already parked in the queue is part of what stop resets, whenever it landed.
		for _, p := range st.pendingInterject {
			if p.MsgID != "" {
				sweep[p.MsgID] = true
			}
		}
		if st.activeSeedMsgID != "" {
			sweep[st.activeSeedMsgID] = true
		}
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// RespondQuestion delivers the user's pick to a waiting ask_user execution.
func (a *App) RespondQuestion(ctx context.Context, c command.RespondQuestion) error {
	// Claiming and delivering happen under one lock: the entry is what says the question is still
	// open, so taking it is what makes this answer THE answer. Two UIs on one daemon can both be
	// looking at this prompt — a browser and a terminal is the arrangement the socket exists for —
	// and the second one has to be told, not thanked.
	a.mu.Lock()
	var ch chan string
	if st, ok := a.stateIf(c.SessionID); ok {
		if ch = st.questions[c.CallID]; ch != nil {
			delete(st.questions, c.CallID)
			delete(st.asking, c.CallID)
		}
	}
	a.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("question %s is not waiting for an answer — it was answered already, "+
			"or the prompt has expired", c.CallID)
	}
	ch <- c.Answer // buffered, and this is the only send that can reach it
	// And told, so a screen that is showing this question and did not answer it can put it away.
	// Two UIs on one daemon is the arrangement the socket exists for, and the one that did not
	// answer was left holding a decision somebody had already made.
	d, _ := json.Marshal(event.QuestionAnsweredData{CallID: c.CallID, Answer: c.Answer})
	a.publishTransient(c.SessionID, event.TypeQuestionAnswered, c.Actor, d)
	return nil
}

// RespondPermission delivers a decision to a waiting tool execution.
func (a *App) RespondPermission(ctx context.Context, c command.RespondPermission) error {
	// Taking the entry IS the decision. Which UI wins a race is not something magi can arbitrate;
	// which one is told it won is, and reporting success to the loser means somebody watches the
	// opposite of what they chose with no reason to doubt their own screen.
	a.mu.Lock()
	var ch chan string
	if st, ok := a.stateIf(c.SessionID); ok {
		if ch = st.perms[c.CallID]; ch != nil {
			delete(st.perms, c.CallID)
			delete(st.asking, c.CallID)
		}
	}
	a.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("permission %s is not waiting for a decision — it was decided already, "+
			"or the prompt has expired", c.CallID)
	}
	ch <- c.Decision // buffered, and this is the only send that can reach it
	return nil
}

// Compact appends a compaction snapshot summarizing the conversation so far.
// Manual /compact replaces the whole conversation with a real model-written brief
// (same summarizer the auto-compaction path uses) plus deterministic per-file recall
// shards, so older detail stays retrievable. If the summary comes back empty (model
// error/empty stream) it returns without appending — a failed summary must never
// replace the context with a stub that wipes it.
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
	if len(msgs) == 0 {
		return nil // nothing to compact
	}
	s := a.sessionInfo(ctx, c.SessionID)
	summary := a.summarizeViaLLM(ctx, a.agentFor(s), s, msgs)
	if summary == "" {
		return fmt.Errorf("compaction skipped: summary unavailable")
	}
	// Manual compaction replaces everything up to upTo, so the post-state is just the
	// summary; the compacted region is indexed by file path so detail stays recallable.
	shards := shardByPath(msgs, s.Workdir)
	data, _ := json.Marshal(event.CompactionData{
		Summary: summary, ReplacesUpToSeq: upTo,
		TokensBefore: estimateTokens("", msgs),
		TokensAfter:  estimateTokens(summary, nil),
		Shards:       shards,
	})
	if err := a.appendFact(ctx, c.SessionID, event.TypeCompaction, c.Actor, data); err != nil {
		return err
	}
	// The measured prompt count now describes the pre-fold window; clear it so the trigger and the
	// TUI /context both re-measure rather than reading the larger, dead value.
	a.setPromptTokens(c.SessionID, 0)
	return nil
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

func (a *App) sessionInfo(ctx context.Context, sid session.SessionID) session.Session {
	a.mu.Lock()
	s, ok := a.metaLocked(sid)
	a.mu.Unlock()
	if ok {
		return s
	}
	// Rebuild from the log if not cached (e.g. resumed session, or ANY read from a second process
	// — the console holds no session state of its own).
	//
	// session.created says what the session opened with, and a model can be changed after that. A
	// rebuild that stopped at the created event reported the opening model for ever: the console's
	// model select repainted it after every successful change, so a switch that had landed looked
	// refused. The newest model.changed wins, which is what SetModel now writes and what the store's
	// own meta scan reads — one fact, one answer, whichever door a reader came through.
	evs, _ := a.store.Read(ctx, sid, 0)
	for _, e := range evs {
		if e.Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			_ = json.Unmarshal(e.Data, &d)
			s = session.Session{ID: sid, Workdir: d.Workdir, Agent: d.Agent, Model: d.Model}
			break
		}
	}
	if s.ID != "" {
		if m := modelFromEvents(evs); m != "" {
			s.Model.Model = m
		}
		a.mu.Lock()
		a.stateLocked(sid).meta = s
		a.mu.Unlock()
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

// modelFromEvents is the session's model as the LOG has it: the newest model.changed, or "" when
// nothing changed it and the created event is still the answer.
//
// One reader of one fact, used by every path that needs it. The alternative — each caller walking
// the events its own way — is how the console ended up reporting the opening model while the
// daemon ran on another: the same question answered from two implementations, one of which had
// never been taught that models move.
func modelFromEvents(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != event.TypeModelChanged {
			continue
		}
		var md event.ModelChangedData
		if json.Unmarshal(evs[i].Data, &md) == nil && md.Model != "" {
			return md.Model
		}
		return ""
	}
	return ""
}
