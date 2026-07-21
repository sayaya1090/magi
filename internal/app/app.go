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
					// COALESCE the whole batch into ONE interjection instead of answering each.
					// Rapid follow-ups queued while a turn ran ("how's it going", then "?", "다시",
					// "aa") are impatience, not N separate tasks — replying to each spams N near-
					// identical answers, badly so when the turn is blocked on background subagents
					// (every one gets the same "still waiting" reply). The most RECENT item survives
					// (that is the prompt the user is actually waiting on) and carries the merged,
					// de-duplicated text; the earlier ones are marked resolved+abandoned so neither
					// seedPromptIdx nor a reload re-runs them.
					a.stateLocked(sid).pendingInterject = nil
					a.mu.Unlock()
					item := q[len(q)-1]
					text := coalesceInterjectionText(q)
					for _, p := range q {
						if p.MsgID == "" || p.MsgID == item.MsgID {
							continue
						}
						ad, _ := json.Marshal(event.PromptAbandonedData{MsgID: p.MsgID})
						_ = a.appendFact(context.WithoutCancel(runCtx), sid, event.TypePromptAbandoned, event.Actor{Kind: event.ActorSystem, ID: "loop"}, ad)
						a.recordDeferral(context.WithoutCancel(runCtx), sid, p.MsgID, true) // resolved: coalesced away
					}
					if text == "" {
						a.recordDeferral(context.WithoutCancel(runCtx), sid, item.MsgID, true)
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
					// Answered inline — already popped and the reply persisted; drain the next batch
					// (any interjection that arrived during triage). Ledger it resolved (F5) so a later
					// reload does not see the deferred entry with no resolution and wrongly re-mask it.
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
			// Mark the cancelled turn's seed prompt abandoned so an unrelated next request
			// isn't anchored onto it (and a follow-up that augments it still seeds on itself).
			a.abandonSeedOnCancel(context.WithoutCancel(runCtx), sid)
			d, _ := json.Marshal(event.TurnFinishedData{})
			_ = a.appendFact(context.WithoutCancel(runCtx), sid, event.TypeTurnFinished,
				event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
		}
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

// Loop stages tag events with the macro phase they belong to (D15).
const (
	stagePlan     = "plan"
	stageExecute  = "execute"
	stageCouncil  = "council"
	stageFinalize = "finalize"
)

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
