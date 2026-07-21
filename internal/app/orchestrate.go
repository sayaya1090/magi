package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// bgGroup tracks the background subagents (sidecars) running for one parent
// session and wakes the parent loop when any completes or the user steers.
type bgGroup struct {
	outstanding int
	wake        chan struct{}   // buffered(1); a signal means "re-read, something changed"
	inflight    map[string]bool // (agent+prompt) keys currently running, for re-dispatch dedup
	completed   map[string]bool // (agent+prompt) keys that have already finished, for serial re-dispatch dedup

	// cancelled records child sessions the orchestrator explicitly cancelled (via
	// cancel_dispatch) and why. injectSubagentResult reads it to render an honest
	// "cancelled by orchestrator" notice with a side-effect manifest for compensation,
	// instead of the generic ctx-cancel error a user Esc / stall-abort produces.
	cancelled map[session.SessionID]string

	// injected/consumed track subagent results that have been written to the
	// session vs. those the orchestrator has already been re-invoked to handle.
	// This is the ordering-independent signal that there are fresh results to
	// synthesize — robust against the race where the orchestrator's own trailing
	// text lands after the async results (which would fool a last-message check).
	injected int
	consumed int

	// asked counts escalations (escalate/ask) raised by this wave's subagents. An
	// escalation is an intermediate signal from the wave — the orchestrator has heard
	// from it — so it lifts cancelDispatched's abuse gate the same way a finished
	// result does. Without this, a subagent that escalates a blocker (e.g. "no git
	// permission") leaves completed/injected at 0, so the orchestrator's decision to
	// cancel the batch and take over is silently refused and the siblings keep running.
	asked int
}

// bgFor returns (creating if needed) the background group for a parent session.
// Caller must hold a.mu.
func (a *App) bgFor(sid session.SessionID) *bgGroup {
	g := a.stateLocked(sid).bg
	if g == nil {
		g = &bgGroup{wake: make(chan struct{}, 1)}
		a.stateLocked(sid).bg = g
	}
	return g
}

// bgWake signals a parent loop waiting on its group (non-blocking).
func (a *App) bgWake(sid session.SessionID) {
	a.mu.Lock()
	g := a.bgFor(sid)
	a.mu.Unlock()
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

// bgOutstanding reports how many background subagents are still running.
func (a *App) bgOutstanding(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok && st.bg != nil {
		return st.bg.outstanding
	}
	return 0
}

// bgWaitChan returns the parent group's wake channel.
func (a *App) bgWaitChan(sid session.SessionID) chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bgFor(sid).wake
}

// bgHasUnconsumed reports whether subagent results have been injected that the
// orchestrator has not yet been re-invoked to synthesize. Ordering-independent,
// so it is immune to the race where the orchestrator's trailing text is appended
// after the async results.
func (a *App) bgHasUnconsumed(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok && st.bg != nil {
		return st.bg.injected > st.bg.consumed
	}
	return false
}

// bgConsume marks all injected subagent results as consumed — called when the
// orchestrator is re-invoked so it won't be woken again for the same results.
func (a *App) bgConsume(sid session.SessionID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok && st.bg != nil {
		st.bg.consumed = st.bg.injected
	}
}

// dispatch runs a subagent in the background (the sidecar model): it returns
// immediately, and when the subagent finishes its result is injected into the
// parent session so the parent agent (kept free like a UI thread) processes it
// at its next step. Used by the task tool for non-blocking delegation.
func (a *App) dispatch(ctx context.Context, parent session.Session, depth int, req port.SpawnRequest) string {
	req.Background = true // dispatched subagents can escalate: the orchestrator stays in its loop to answer
	key := req.Agent + "\x00" + req.Prompt
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return "" // shutting down: don't spawn a new background subagent
	}
	g := a.bgFor(parent.ID)
	if g.inflight == nil {
		g.inflight = map[string]bool{}
	}
	if g.inflight[key] {
		// An identical task for this agent is already running in the background.
		// Refuse the re-dispatch so a weak model that forgot it already delegated
		// doesn't spawn a duplicate; its result is still on the way.
		a.mu.Unlock()
		return req.Agent + " is already running in the background with this exact task — not re-dispatched; its result will arrive as a message. Wait for it instead of delegating again."
	}
	if g.completed[key] {
		// An identical task for this agent already FINISHED. A delegate is context-free,
		// so re-running the same prompt only reproduces the same result — a weak orchestrator
		// that keeps re-delegating an identical degenerate task (e.g. handing "hi" to a coder
		// over and over because the reply wasn't the review it wanted) would livelock here,
		// each completion re-waking it to dispatch the same thing again. Refuse: the finished
		// result is already in the conversation. To make progress the model must USE that
		// result or send a DIFFERENT prompt.
		a.mu.Unlock()
		return req.Agent + " already ran with this exact task and its result is already in the conversation above — re-running it verbatim would only reproduce the same output. Use that result, or dispatch with a DIFFERENT, more specific prompt. Not re-dispatched."
	}
	g.inflight[key] = true
	g.outstanding++
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		// An overall req.Timeout bounds every restart attempt of this background spawn.
		// It expires as a parent-ctx cancellation, which runAttempt classifies as
		// terminal (not a per-attempt timeout), so an expired spawn is NOT retried —
		// the error result is injected below and the parent moves on.
		sctx := ctx
		if req.Timeout > 0 {
			var scancel context.CancelFunc
			sctx, scancel = context.WithTimeout(ctx, req.Timeout)
			defer scancel()
		}
		res := a.spawn(sctx, parent, depth, req)
		// Inject the result as a message on the parent so the orchestrator picks it
		// up incrementally (partial results, not all-or-nothing).
		a.injectSubagentResult(ctx, parent.ID, req.Agent, res)
		a.mu.Lock()
		if g := a.stateLocked(parent.ID).bg; g != nil {
			delete(g.inflight, key)
			if g.completed == nil {
				g.completed = map[string]bool{}
			}
			g.completed[key] = true // remember it finished, so an identical serial re-dispatch is refused
			if g.outstanding > 0 {
				g.outstanding--
			}
			g.injected++ // a fresh result the orchestrator hasn't synthesized yet
		}
		a.mu.Unlock()
		// Wake the orchestrator so it re-reads. We do NOT inject an "all subagents
		// reported, STOP" nudge here: that fired on the FIRST batch reaching zero
		// even when the orchestrator meant to dispatch more waves, prematurely
		// ending the turn. Instead the loop's needsOrchestratorTurn re-invokes the
		// orchestrator when outstanding hits 0, and IT decides to synthesize or
		// dispatch more — multi-wave delegation works, and the loop guard backs it.
		a.bgWake(parent.ID)
	}()
	return ""
}

// escalate lets a running subagent ask its orchestrator (parent) for something
// it can't get itself — the question is injected into the parent session and the
// subagent blocks until the orchestrator's next reply, which is routed back as
// the answer. This is the general "request anything mid-task" channel.
func (a *App) escalate(ctx context.Context, parent session.SessionID, fromRole, question string) (string, error) {
	if parent == "" {
		return "", fmt.Errorf("no orchestrator to ask")
	}
	ch := make(chan string, 1)
	a.mu.Lock()
	if a.stateLocked(parent).pendingAsk != nil {
		a.mu.Unlock()
		return "", fmt.Errorf("the orchestrator is already handling another question; try again shortly")
	}
	a.stateLocked(parent).pendingAsk = ch
	// An escalation is an intermediate signal from the wave: it lets the orchestrator
	// legitimately cancel the batch (cancelDispatched) even before any subagent finishes.
	if g := a.stateLocked(parent).bg; g != nil {
		g.asked++
	}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		// Only clear OUR entry: if answerPendingAsk already consumed it and another
		// subagent has since registered its own ch, deleting unconditionally would
		// orphan that second subagent (it'd block until its 2-minute timeout).
		if a.stateLocked(parent).pendingAsk == ch {
			a.stateLocked(parent).pendingAsk = nil
		}
		a.mu.Unlock()
	}()

	text := fmt.Sprintf("[subagent %s asks] %s\n\n(Answer this concisely — your reply is sent straight back to the subagent so it can continue. Don't re-dispatch.)", fromRole, question)
	if err := a.appendPromptText(context.WithoutCancel(ctx), parent,
		event.Actor{Kind: event.ActorAgent, ID: "subagent:" + fromRole}, text); err != nil {
		return "", err
	}
	a.bgWake(parent) // wake the orchestrator if it's parked in the bg-wait

	select {
	case ans := <-ch:
		return ans, nil
	case <-time.After(2 * time.Minute):
		return "", fmt.Errorf("timed out waiting for the orchestrator's answer")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// answerPendingAsk routes an orchestrator's assistant reply back to a subagent
// blocked in escalate(). Returns true if it consumed the reply as an answer.
func (a *App) answerPendingAsk(sid session.SessionID, reply string) bool {
	a.mu.Lock()
	ch := a.stateLocked(sid).pendingAsk
	if ch != nil {
		a.stateLocked(sid).pendingAsk = nil
	}
	a.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- reply:
	default:
	}
	return true
}

// needsOrchestratorTurn reports whether the orchestrator should be (re)invoked
// while background subagents run. It returns true ONLY for things the model must
// act on — a subagent asking (escalation), a real user steer, or all subagents
// done with results to synthesize — NOT for each individual subagent result as
// it arrives (those just accumulate). This keeps weak models from re-invoking
// per-completion and fabricating/re-dispatching.
func (a *App) needsOrchestratorTurn(ctx context.Context, sid session.SessionID) bool {
	a.mu.Lock()
	ask := a.stateLocked(sid).pendingAsk != nil
	a.mu.Unlock()
	if ask {
		return true
	}
	if a.lastIsUserSteer(ctx, sid) {
		return true
	}
	// All subagents finished and there are fresh results to act on → synthesize,
	// once. bgHasUnconsumed is the ordering-independent signal (set when a result
	// is injected, cleared when the orchestrator is re-invoked); hasUnansweredUserPrompt
	// is kept as a belt-and-suspenders check for the common last-message-is-result case.
	return a.bgOutstanding(sid) == 0 && (a.bgHasUnconsumed(sid) || a.hasUnansweredUserPrompt(ctx, sid, a.deferredInterjectIDs(sid)))
}

// lastIsUserSteer reports whether the most recent message-bearing event is a
// prompt submitted by the actual USER (a steer) — not a subagent result/nudge.
// Currently-DEFERRED interjections are excluded: a queued interjection is one the
// loop has decided NOT to act on now (it runs as its own later turn), so it must not
// wake the orchestrator out of its bg-wait/early-park. A dispatch-visible interjection
// (never queued) is not deferred, so it still counts — that is the wake we want.
func (a *App) lastIsUserSteer(ctx context.Context, sid session.SessionID) bool {
	raw, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return false
	}
	evs := a.liveEvents(sid, raw) // drop currently-queued interjections
	for i := len(evs) - 1; i >= 0; i-- {
		switch evs[i].Type {
		case event.TypePromptSubmitted:
			return evs[i].Actor.Kind == event.ActorUser
		case event.TypePartAppended:
			return false // an assistant/tool message is newer than any user prompt
		}
	}
	return false
}

// injectSubagentResult appends a subagent's result to the parent session as a
// user-role prompt so the orchestrator reads and acts on it.
func (a *App) injectSubagentResult(ctx context.Context, parent session.SessionID, agentName string, res port.SpawnResult) {
	// Was this child cancelled by the orchestrator (cancel_dispatch)? If so, render an
	// honest cancel notice with a manifest of what it already did, so the orchestrator can
	// run its own compensating (undo) actions — nothing is auto-rolled back (R0).
	a.mu.Lock()
	var cancelReason string
	var cancelled bool
	if g := a.stateLocked(parent).bg; g != nil {
		cancelReason, cancelled = g.cancelled[res.SessionID]
	}
	a.mu.Unlock()

	var text string
	if cancelled {
		text = fmt.Sprintf("[subagent %s cancelled by orchestrator: %s]\n"+
			"It did NOT finish and nothing was auto-rolled back. Actions it performed before "+
			"cancel (undo any that must not persist, using your own tools — this is your "+
			"compensating step):\n%s\n"+
			"If none of these need undoing, ignore this and synthesize from the results you kept.",
			agentName, cancelReason, a.subagentActionManifest(ctx, res.SessionID))
	} else {
		body := res.Text
		if res.Err != "" {
			body = "ERROR: " + res.Err
		} else if strings.TrimSpace(body) == "" {
			body = "(no output — the subagent finished without producing a result; re-dispatch with clearer instructions or handle this yourself)"
		}
		text = fmt.Sprintf("[subagent %s result]\n%s", agentName, body)
	}
	// Tell the orchestrator what's still pending so it waits for the rest (rather
	// than re-dispatching) and knows when it can synthesize. Self is still counted
	// here (decremented after injection), so subtract it.
	if remaining := a.bgOutstanding(parent) - 1; remaining > 0 {
		text += fmt.Sprintf("\n\n(%d other subagent(s) still running — wait for them before synthesizing; don't re-dispatch.)", remaining)
	}
	// Detached context: the parent turn's ctx may already be winding down.
	_ = a.appendPromptText(context.WithoutCancel(ctx), parent,
		event.Actor{Kind: event.ActorAgent, ID: "subagent:" + agentName}, text)
	// A leaf subagent runs at depth>0 and never convenes a council, so its structural
	// signals would die at this boundary — the parent would read only the child's prose.
	// Fold the child's ledger and re-raise still-open concerns onto the parent, scoped to
	// the child agent, so the parent council sees them as first-class evidence.
	a.bubbleSubagentConcerns(context.WithoutCancel(ctx), parent, agentName, res.SessionID)
}

// subagentActionManifest summarizes the tool actions a (usually cancelled) child ran, so
// the orchestrator can decide which side effects to compensate for. Best-effort: a read
// failure or an action-free child yields a placeholder, never an error.
func (a *App) subagentActionManifest(ctx context.Context, child session.SessionID) string {
	if child == "" {
		return "(no actions recorded)"
	}
	evs, err := a.store.Read(context.WithoutCancel(ctx), child, 0)
	if err != nil {
		return "(actions unavailable — could not read the subagent's log)"
	}
	if m := turnToolEvidence(evs, 12); m != "" {
		return m
	}
	return "(no side-effecting actions recorded)"
}

// cancelDispatched cancels the parent orchestrator's still-running BACKGROUND subagents.
// agentFilter=="" cancels all remaining; otherwise only that role. It refuses when the
// current wave has produced no signal at all — no finished result AND no escalation (an
// ask is the orchestrator hearing from the wave, so it counts). The feature exists to
// drop siblings an INTERMEDIATE result or blocker made unnecessary; cancelling a wave
// you've seen nothing from is abuse. It also refuses when reason is empty (intent must
// be recorded). Cancelling flows through the
// normal dispatch cleanup: each child's ctx.Done unwinds spawn, whose result is injected
// with a compensation manifest and decrements outstanding. Returns the count cancelled.
func (a *App) cancelDispatched(ctx context.Context, parent session.SessionID, agentFilter, reason string) (int, error) {
	if strings.TrimSpace(reason) == "" {
		return 0, fmt.Errorf("cancel needs a reason — say why the remaining subagents are no longer needed")
	}
	a.mu.Lock()
	g := a.stateLocked(parent).bg
	if g == nil || (len(g.completed) == 0 && g.injected == 0 && g.asked == 0) {
		a.mu.Unlock()
		return 0, fmt.Errorf("no intermediate result has arrived yet — wait for at least one subagent's result (or escalation) before deciding the rest are unnecessary")
	}
	if g.cancelled == nil {
		g.cancelled = map[session.SessionID]string{}
	}
	var cancels []func()
	for id, st := range a.states {
		s := st.meta
		if s.Parent != parent || !s.Escalatable {
			continue // only background-dispatched children of this orchestrator
		}
		if agentFilter != "" && s.Agent != agentFilter {
			continue
		}
		c := st.cancel
		if c == nil {
			continue // already finished / not running
		}
		g.cancelled[id] = reason
		cancels = append(cancels, c)
	}
	a.mu.Unlock()

	// Each cancelled child injects its own honest "cancelled by orchestrator: <reason>"
	// notice into the parent log (injectSubagentResult), so repeated/abusive cancels are
	// already visible there — no separate audit fact is needed.
	for _, c := range cancels {
		c() // child ctx.Done → spawn unwinds → injectSubagentResult renders the cancel notice
	}
	return len(cancels), nil
}

// bubbleSubagentConcerns carries a finished child's open concerns across the subagent
// boundary. It re-keys each by child agent (subagent:<name>/<childKey>) so one child's
// concerns stay distinct from the parent's own and from other children, embeds the
// provenance in Detail (council evidence is prose-only, it never sees Scope), and skips
// keys already open on the parent so a re-injection cannot duplicate. Best-effort: a read
// failure means no bubble-up, never a spawn failure.
func (a *App) bubbleSubagentConcerns(ctx context.Context, parent session.SessionID, agentName string, child session.SessionID) {
	if child == "" {
		return
	}
	childEvs, err := a.store.Read(ctx, child, 0)
	if err != nil {
		return
	}
	open := sessionConcerns(childEvs)
	if len(open) == 0 {
		return
	}
	present := map[string]bool{}
	if pevs, perr := a.store.Read(ctx, parent, 0); perr == nil {
		for _, c := range sessionConcerns(pevs) {
			present[c.Key] = true
		}
	}
	scope := "subagent:" + agentName
	for _, c := range open {
		key := scope + "/" + c.Key
		if present[key] {
			continue
		}
		detail := fmt.Sprintf("[via subagent %s] %s", agentName, c.Detail)
		_ = a.appendConcernRaised(ctx, parent,
			event.Actor{Kind: event.ActorAgent, ID: scope},
			key, c.Source, c.Kind, c.Status, detail, scope)
	}
}

// cloneConversation copies the parent's reconstructable conversation into the child
// session, so a refine child re-plans its sub-goal WITH the full context carried forward
// — the in-context property that distinguishes refine from a context-free delegate. Only
// the event types reconstruct() turns into messages are copied (PromptSubmitted /
// PartAppended / Compaction): exactly what the model sees, nothing else. Append re-stamps
// Seq/SessionID (jsonl.Store), so the copies become the child's own history. Best-effort:
// on a read error the child simply starts context-light rather than failing the spawn.
func (a *App) cloneConversation(ctx context.Context, from, to session.SessionID) {
	evs, err := a.store.Read(ctx, from, 0)
	if err != nil {
		return
	}
	clone := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			// reconstruct maps EVERY prompt to RoleUser regardless of actor, so a verbatim
			// clone leaves the parent's own turn-driving messages indistinguishable from the
			// child's delegated task — the child then answers a request that was never its
			// own (field report: a cloned child executing the parent's original user prompt).
			// Clean division: keep the WORK (assistant/tool events below), but strip the
			// parent's steering prompts so only the seed appended after the clone reads as an
			// instruction.
			switch e.Actor.Kind {
			case event.ActorUser:
				// The parent's ORIGINAL user request. Reframe it to a background marker
				// (keeps a valid leading RoleUser message for the provider) rather than
				// letting its verbatim wording pull the child off its delegated unit.
				if reframed, ok := reframeInheritedPrompt(e.Data); ok {
					clone = append(clone, event.Event{Type: e.Type, Actor: e.Actor, Data: reframed})
				}
			case event.ActorSystem:
				// Parent-turn system steering (planner spec/checkpoint/mining notes, loop
				// resubmissions): scoped to the parent's turn and pure context weight — the
				// child re-derives its own notes at plan time. Drop them.
			default:
				// ActorAgent: subagent results and other inherited work — keep as context.
				clone = append(clone, event.Event{Type: e.Type, Actor: e.Actor, Data: e.Data})
			}
		case event.TypePartAppended, event.TypeCompaction:
			clone = append(clone, event.Event{Type: e.Type, Actor: e.Actor, Data: e.Data})
		}
	}
	if len(clone) > 0 {
		_, _ = a.store.Append(ctx, to, clone...)
	}
}

// inheritedContextHeader replaces a cloned parent user prompt so a CloneContext child reads it as
// background, not as its own instruction. The child's real task is the seed prompt appended after
// the clone; this leaves the earlier request visible only as a labelled context boundary.
const inheritedContextHeader = "[Inherited context — an earlier request handled by the agent that " +
	"dispatched you. Shown for background only; do NOT answer or restart it. Your task is the most " +
	"recent instruction at the end of this conversation.]"

// reframeInheritedPrompt rewrites a cloned user prompt to the inherited-context header, preserving
// the MessageID (and thus a valid leading RoleUser message) while dropping the verbatim request
// that a weak child would mistake for its own task. Idempotent: an already-reframed prompt (nested
// clone) is returned unchanged. Returns false when the payload can't be parsed, so the caller skips
// it entirely.
func reframeInheritedPrompt(data []byte) ([]byte, bool) {
	var d event.PromptSubmittedData
	if json.Unmarshal(data, &d) != nil {
		return nil, false
	}
	if len(d.Parts) == 1 && d.Parts[0].Kind == session.PartText && d.Parts[0].Text == inheritedContextHeader {
		return data, true // already reframed (nested clone)
	}
	d.Parts = []session.Part{{Kind: session.PartText, Text: inheritedContextHeader}}
	out, err := json.Marshal(d)
	if err != nil {
		return nil, false
	}
	return out, true
}

// spawn runs a named subagent to completion and returns its output. It is the
// blocking, supervised core (D7 recursion caps + sidecar liveness): each attempt
// has a hard timeout and a stall watchdog, and stalls/timeouts/transient errors
// are retried up to SubagentMaxRestarts.
func (a *App) spawn(ctx context.Context, parent session.Session, depth int, req port.SpawnRequest) port.SpawnResult {
	spec, ok := a.resolveAgentSpec(req.Agent)
	if !ok {
		// Same failure shape as an unknown TOOL: told only that the name is unknown,
		// the model invents another stereotypical role (analyst, report-writer) or
		// silently gives up on delegating. Name the actual roster so one rejection
		// converts into a valid call — or an informed decision to do the work itself.
		roster := "none are configured — do the work yourself"
		if names := a.AgentNames(); len(names) > 0 {
			roster = strings.Join(names, ", ")
		}
		return port.SpawnResult{Err: "unknown agent: " + req.Agent + ". Available agents: " + roster +
			". Do not invent agent names; if none fits, do the work yourself."}
	}
	return a.spawnResolved(ctx, parent, depth, spec, req)
}

// spawnResolved is spawn's core with the agent spec already resolved. It exists so the
// stuck-recovery lifeline (redecomposeStuck) can re-run the *main orchestrator's own* spec —
// which is built on the fly by agentFor and is deliberately absent from cfg.Agents, so a
// name lookup via resolveAgentSpec would fail with "unknown agent". Recovery is the main
// agent doing the work itself, not a handoff to a registered subagent.
func (a *App) spawnResolved(ctx context.Context, parent session.Session, depth int, spec AgentSpec, req port.SpawnRequest) port.SpawnResult {
	if depth+1 > a.cfg.MaxDepth {
		return port.SpawnResult{Err: fmt.Sprintf("max depth reached (%d)", a.cfg.MaxDepth)}
	}
	if a.spawnCount.Add(1) > int64(a.cfg.MaxAgents) {
		return port.SpawnResult{Err: fmt.Sprintf("agent budget exhausted (%d)", a.cfg.MaxAgents)}
	}

	// Work-tree checkpoint (opt-in): snapshot before the first attempt so a RETRY rolls back to a
	// clean tree instead of re-running on the failed attempt's debris (compile-compcert self-clone
	// retry loop). Bounded to this subagent's own retry loop — a solo execution boundary, so a
	// sibling's tree is never rolled back under it. Best-effort; nil when unavailable or disabled.
	var cp *workdirCheckpoint
	if workdirCheckpointEnabled() && a.cfg.SubagentMaxRestarts > 0 {
		cp = newWorkdirCheckpoint(parent.Workdir)
		defer cp.cleanup()
	}

	var last port.SpawnResult
	for attempt := 0; attempt <= a.cfg.SubagentMaxRestarts; attempt++ {
		if ctx.Err() != nil {
			return port.SpawnResult{Err: ctx.Err().Error()}
		}
		attemptReq := req
		if attempt > 0 {
			a.emitAgentRestart(parent.ID, spec.Name, attempt, last.Err)
			// Roll the work-tree back to the pre-attempt checkpoint (if any) so the retry starts
			// from the same clean state the first attempt did, not the debris it left behind.
			if cp != nil {
				if err := cp.restore(); err == nil {
					a.emitToolProgress(parent.ID, event.Actor{Kind: event.ActorAgent, ID: orDefault(parent.Agent, "default")},
						"", spec.Name, "rolled the work tree back to the pre-attempt checkpoint")
				}
			}
			// A restart with the IDENTICAL prompt and seed re-runs the identical
			// failure: observed on compile-compcert, a recovery child timed out in a
			// long toolchain install three times in a row, each attempt walking the
			// same path into the same wall (the "self-clone retry loop" field report).
			// Tell the retry what happened — the failure reason plus a digest of the
			// previous attempt's tool trail — and require a DIFFERENT route.
			attemptReq.Prompt = req.Prompt + retryPivotNote(ctx, a, last, attempt)
		}
		res, retry := a.runAttempt(ctx, parent, depth, spec, attemptReq)
		if !retry {
			return res
		}
		last = res
	}
	return port.SpawnResult{Err: fmt.Sprintf("%s (failed after %d attempts)", last.Err, a.cfg.SubagentMaxRestarts+1)}
}

// runAttempt runs a single supervised subagent attempt. retry is true when the
// attempt stalled or hit its hard timeout (the supervisor should restart it).
func (a *App) runAttempt(ctx context.Context, parent session.Session, depth int, spec AgentSpec, req port.SpawnRequest) (res port.SpawnResult, retry bool) {
	// Throttle only TOP-LEVEL fan-out (depth 0). A parent holds its slot for the whole
	// lifetime of its synchronous child, so if nested subagents also took slots, a
	// full top-level fan-out where each child delegates would deadlock — every slot
	// held by a parent waiting for a child that can't get a slot. Nested concurrency
	// is still bounded by MaxDepth and the cumulative MaxAgents cap.
	if depth == 0 {
		select {
		case a.sem <- struct{}{}:
		case <-ctx.Done():
			return port.SpawnResult{Err: ctx.Err().Error()}, false
		}
		defer func() { <-a.sem }()
	}

	model := parent.Model
	if spec.Model != (session.ModelRef{}) {
		model = spec.Model
	}
	actor := event.Actor{Kind: event.ActorAgent, ID: orDefault(parent.Agent, "default")}

	// ReuseSession (refine shared-session): continue a prior attempt's session instead of
	// creating a fresh one — skip creation, the SessionCreated event, and CloneContext, since
	// the accumulated conversation already IS the context. This runs sequentially-dependent
	// refine phases in ONE child so each sees its predecessors' actual work. Falls back to a
	// fresh session if the reuse target has vanished (defensive; the caller passes back a
	// SessionID it just received).
	var child session.Session
	if req.ReuseSession != "" {
		a.mu.Lock()
		c, ok := a.metaLocked(req.ReuseSession)
		a.mu.Unlock()
		if ok {
			child = c
		}
	}
	if child.ID == "" {
		child = session.Session{
			ID:          session.SessionID("s_" + newID()),
			Workdir:     parent.Workdir,
			Agent:       spec.Name,
			Parent:      parent.ID,
			ParentStep:  req.PlanStepIndex, // plan-step this child serves (delegate/refine); nil otherwise
			Model:       model,
			Created:     time.Now(),
			Escalatable: req.Background, // only background-dispatched children can be answered
		}
		a.mu.Lock()
		a.stateLocked(child.ID).meta = child
		a.mu.Unlock()

		cd, _ := json.Marshal(event.SessionCreatedData{
			Workdir: child.Workdir, Agent: spec.Name, Model: model, Parent: string(parent.ID),
			ParentStep: req.PlanStepIndex,
		})
		a.appendFact(ctx, child.ID, event.TypeSessionCreated, actor, cd)

		// refine (in-context recursion): seed the child with the parent's conversation so its
		// pre-flight planner works out the sub-goal WITH the full context carried forward,
		// instead of the context-free hand-off delegate uses. Done after SessionCreated and
		// before the sub-goal prompt so the prompt is the most-recent message.
		// A ReuseSession that missed (target gone from a.sessions — only possible if session
		// eviction is ever added) lands here too: clone as well, so the fallback child degrades
		// to the legacy spawn-time clone rather than getting neither reuse nor context.
		if req.CloneContext || req.ReuseSession != "" {
			a.cloneConversation(ctx, parent.ID, child.ID)
		}
	}

	// A stuck-recovery child starts flagged as already-recovered so it cannot fire its own
	// redecomposeStuck (run-tree recovery cap). Marked on the child state, consumed by runLoop.
	if req.Recovery {
		a.mu.Lock()
		a.stateLocked(child.ID).recoverySeed = true
		a.mu.Unlock()
	}

	msgID := "m_" + newID()
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: msgID,
		Parts:     []session.Part{{Kind: session.PartText, Text: req.Prompt}},
	})
	a.appendFact(ctx, child.ID, event.TypePromptSubmitted, actor, pd)
	// Record the seed deterministically: a CloneContext child's event log also contains
	// the parent's ORIGINAL user prompts (actors preserved), so "last ActorUser prompt"
	// resolves to a STALE parent request there — the child's nudges/council/criteria then
	// anchor on the wrong task (field report: a cloned child appearing to execute an
	// earlier prompt instead of its spawn task). seedTurnTask prefers this record.
	a.mu.Lock()
	a.stateLocked(child.ID).seedPrompt = req.Prompt
	a.mu.Unlock()
	a.touch(child.ID) // seed liveness so the watchdog doesn't fire immediately

	// Run the subagent under a judgment lease: the elastic cap (attemptCap, see
	// subagent_cap.go) is the INITIAL lease; when it expires the orchestrator's
	// model judges the child's activity digest — churn is killed, real progress
	// earns an extension — bounded by the absolute backstop (subagent_judge.go).
	// Cancellation is ours (not a ctx deadline) so lease decisions and user
	// interrupts share one path; capExpired marks OUR kills as retryable.
	// Register the cancel so the user can interrupt this specific subagent
	// (Esc on its focused pane).
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attemptStart := time.Now()
	backstop := a.subagentBackstop()
	capExpired := false
	capTimer := time.NewTimer(a.attemptCap(model.Model))
	defer capTimer.Stop()
	a.mu.Lock()
	a.stateLocked(child.ID).cancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.stateLocked(child.ID).cancel = nil
		a.mu.Unlock()
	}()

	// Announce the spawn on the parent session for the live pane view. Published
	// AFTER the cancel is registered so an Interrupt issued in response to THIS
	// event (a UI/test cancelling the just-spawned subagent) always finds a live
	// cancel func. Announcing first raced the registration: the interrupt would
	// read a nil cancel, no-op, and the subagent would run to its full timeout.
	sd, _ := json.Marshal(event.AgentStatusData{
		AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name, State: "running",
	})
	a.publishTransient(parent.ID, event.TypeAgentSpawned, actor, sd)

	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		t, e := a.runLoop(attemptCtx, child, spec, depth+1, req.MaxSteps, false)
		done <- outcome{t, e}
	}()

	announceDone := func() {
		d, _ := json.Marshal(event.AgentStatusData{
			AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name, State: "done",
		})
		a.publishTransient(parent.ID, event.TypeAgentStatus, actor, d)
		// Guarantee the child's live pane freezes at THIS boundary, whatever runLoop
		// exit path it took. The clean finish emits its own persisted TurnFinished, but
		// a cancel/timeout/stall or a severed stream returns without one (and, unlike the
		// top-level run, a child has no fallback emitter) — so the pane's timer would run
		// until the parent turn's sweep. A transient TurnFinished on the child sid isn't
		// ctx-gated, so it lands even when the child was cancelled; the pane treats a
		// repeat as an idempotent no-op.
		fd, _ := json.Marshal(event.TurnFinishedData{})
		a.publishTransient(child.ID, event.TypeTurnFinished, actor, fd)
	}

	ticker := time.NewTicker(maxDur(a.cfg.SubagentStall/3, time.Second))
	defer ticker.Stop()
	for {
		select {
		case o := <-done:
			announceDone()
			if o.err != nil {
				// Retry only when OUR lease/backstop kill fired (not a parent
				// cancellation or user interrupt, which propagate as terminal).
				timedOut := capExpired && ctx.Err() == nil
				return port.SpawnResult{Err: o.err.Error(), SessionID: child.ID}, timedOut
			}
			return port.SpawnResult{Text: o.text, SessionID: child.ID}, false

		case <-capTimer.C:
			// Lease expiry. Judge (or, with judging disabled, kill outright): an
			// extension is clamped so the attempt can never outlive the backstop,
			// and a spent backstop skips the judge entirely — no verdict can add
			// time that doesn't exist. The child keeps running while the judge
			// deliberates; a kill verdict cancels it exactly like the old hard cap.
			ext, verdictNote := time.Duration(0), "backstop spent"
			if backstop > time.Since(attemptStart) {
				ext, verdictNote = a.judgeLease(ctx, parent, child, req.Prompt, time.Since(attemptStart))
			}
			// Clamp AFTER the judge call: the verdict can take up to judgeCallTimeout,
			// and the extension clock only starts at the Reset below, so a pre-call
			// remainder would let each judged extension overshoot the backstop by the
			// judge's own latency.
			if rem := backstop - time.Since(attemptStart); ext > rem {
				ext = rem
			}
			if ext <= 0 {
				// The child kept running while the judge deliberated; if it FINISHED
				// in that window its outcome — possibly a success — beats the lease
				// verdict. Never discard a completed attempt.
				select {
				case o := <-done:
					announceDone()
					if o.err != nil {
						return port.SpawnResult{Err: o.err.Error(), SessionID: child.ID}, false
					}
					return port.SpawnResult{Text: o.text, SessionID: child.ID}, false
				default:
				}
				// Transparency: WHY the lease ended (v11 forensics had kills with no
				// visible reason — undiagnosable from the run alone).
				kd, _ := json.Marshal(event.AgentStatusData{
					AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name,
					State: "lease expired (" + verdictNote + ")",
				})
				a.publishTransient(parent.ID, event.TypeAgentStatus, actor, kd)
				capExpired = true
				cancel()
				<-done
				announceDone()
				return port.SpawnResult{Err: "subagent lease expired (" + verdictNote + ")", SessionID: child.ID}, true
			}
			ld, _ := json.Marshal(event.AgentStatusData{
				AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name,
				State: fmt.Sprintf("lease extended +%s (judged in progress)", fmtElapsed(ext)),
			})
			a.publishTransient(parent.ID, event.TypeAgentStatus, actor, ld)
			capTimer.Reset(ext)

		case <-ticker.C:
			// Stall = no event activity for SubagentStall AND no tool in flight. The
			// tool guard keeps a silent long-running tool (a bash build emits nothing
			// until it returns) from being mistaken for a wedged child; such a tool is
			// bounded by its own timeout, and a genuine hang past that by the hard cap.
			if a.idleFor(child.ID) > a.cfg.SubagentStall && !a.toolInFlight(child.ID) {
				cancel() // abort the stalled attempt
				<-done   // let runLoop unwind
				announceDone()
				return port.SpawnResult{Err: "subagent stalled (no activity)", SessionID: child.ID}, true
			}

		case <-ctx.Done():
			cancel()
			<-done
			announceDone()
			return port.SpawnResult{Err: ctx.Err().Error(), SessionID: child.ID}, false
		}
	}
}

// seedPromptOf returns the spawn/unit prompt a subagent session was seeded with
// ("" for top-level sessions or children spawned before this record existed).
func (a *App) seedPromptOf(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.seedPrompt
	}
	return ""
}

// retryPivotNote builds the strategy-change directive appended to a restarted
// attempt's prompt: the previous attempt's failure reason plus a digest of its
// tool trail (when its session is readable), and an explicit requirement to take
// a different route — retrying the identical plan against the same wall is how a
// timeout loop burns every restart. Best-effort: with no readable trail it still
// names the failure and demands a pivot.
func retryPivotNote(ctx context.Context, a *App, last port.SpawnResult, attempt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n# Retry %d — previous attempt failed: %s\n", attempt, strings.TrimSpace(last.Err))
	if last.SessionID != "" {
		if evs, err := a.store.Read(ctx, last.SessionID, 0); err == nil {
			if d := childToolDigest(evs, 12); d != "" {
				b.WriteString("Its tool trail (what has ALREADY been tried — do not walk this path again):\n" + d + "\n")
			}
		}
	}
	b.WriteString("Take a DIFFERENT route this time: change the approach, not just the parameters " +
		"(a faster variant, a prebuilt artifact, a workaround flag, or a smaller first deliverable). " +
		"If a single step needs longer than this attempt's whole budget, do the smallest useful part " +
		"first and report what remains — do NOT restart the same long-running path.")
	return b.String()
}

// emitAgentRestart announces a subagent restart on the parent for visibility.
func (a *App) emitAgentRestart(parent session.SessionID, role string, attempt int, reason string) {
	d, _ := json.Marshal(event.AgentStatusData{
		Parent: string(parent), Role: role,
		State: fmt.Sprintf("restarting (attempt %d): %s", attempt+1, reason),
	})
	a.publishTransient(parent, event.TypeAgentStatus, event.Actor{Kind: event.ActorSystem, ID: "supervisor"}, d)
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
