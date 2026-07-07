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

	// injected/consumed track subagent results that have been written to the
	// session vs. those the orchestrator has already been re-invoked to handle.
	// This is the ordering-independent signal that there are fresh results to
	// synthesize — robust against the race where the orchestrator's own trailing
	// text lands after the async results (which would fool a last-message check).
	injected int
	consumed int
}

// bgFor returns (creating if needed) the background group for a parent session.
// Caller must hold a.mu.
func (a *App) bgFor(sid session.SessionID) *bgGroup {
	g := a.bg[sid]
	if g == nil {
		g = &bgGroup{wake: make(chan struct{}, 1)}
		a.bg[sid] = g
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
	if g := a.bg[sid]; g != nil {
		return g.outstanding
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
	if g := a.bg[sid]; g != nil {
		return g.injected > g.consumed
	}
	return false
}

// bgConsume marks all injected subagent results as consumed — called when the
// orchestrator is re-invoked so it won't be woken again for the same results.
func (a *App) bgConsume(sid session.SessionID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if g := a.bg[sid]; g != nil {
		g.consumed = g.injected
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
		res := a.spawn(ctx, parent, depth, req)
		// Inject the result as a message on the parent so the orchestrator picks it
		// up incrementally (partial results, not all-or-nothing).
		a.injectSubagentResult(ctx, parent.ID, req.Agent, res)
		a.mu.Lock()
		if g := a.bg[parent.ID]; g != nil {
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
	if a.pendingAsks[parent] != nil {
		a.mu.Unlock()
		return "", fmt.Errorf("the orchestrator is already handling another question; try again shortly")
	}
	a.pendingAsks[parent] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		// Only clear OUR entry: if answerPendingAsk already consumed it and another
		// subagent has since registered its own ch, deleting unconditionally would
		// orphan that second subagent (it'd block until its 2-minute timeout).
		if a.pendingAsks[parent] == ch {
			delete(a.pendingAsks, parent)
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
	ch := a.pendingAsks[sid]
	if ch != nil {
		delete(a.pendingAsks, sid)
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
	ask := a.pendingAsks[sid] != nil
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
	return a.bgOutstanding(sid) == 0 && (a.bgHasUnconsumed(sid) || a.hasUnansweredUserPrompt(ctx, sid))
}

// lastIsUserSteer reports whether the most recent message-bearing event is a
// prompt submitted by the actual USER (a steer) — not a subagent result/nudge.
func (a *App) lastIsUserSteer(ctx context.Context, sid session.SessionID) bool {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return false
	}
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
	body := res.Text
	if res.Err != "" {
		body = "ERROR: " + res.Err
	} else if strings.TrimSpace(body) == "" {
		body = "(no output — the subagent finished without producing a result; re-dispatch with clearer instructions or handle this yourself)"
	}
	text := fmt.Sprintf("[subagent %s result]\n%s", agentName, body)
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
		case event.TypePromptSubmitted, event.TypePartAppended, event.TypeCompaction:
			clone = append(clone, event.Event{Type: e.Type, Actor: e.Actor, Data: e.Data})
		}
	}
	if len(clone) > 0 {
		_, _ = a.store.Append(ctx, to, clone...)
	}
}

// spawn runs a named subagent to completion and returns its output. It is the
// blocking, supervised core (D7 recursion caps + sidecar liveness): each attempt
// has a hard timeout and a stall watchdog, and stalls/timeouts/transient errors
// are retried up to SubagentMaxRestarts.
func (a *App) spawn(ctx context.Context, parent session.Session, depth int, req port.SpawnRequest) port.SpawnResult {
	if depth+1 > a.cfg.MaxDepth {
		return port.SpawnResult{Err: fmt.Sprintf("max depth reached (%d)", a.cfg.MaxDepth)}
	}
	if a.spawnCount.Add(1) > int64(a.cfg.MaxAgents) {
		return port.SpawnResult{Err: fmt.Sprintf("agent budget exhausted (%d)", a.cfg.MaxAgents)}
	}
	spec, ok := a.resolveAgentSpec(req.Agent)
	if !ok {
		return port.SpawnResult{Err: "unknown agent: " + req.Agent}
	}

	var last port.SpawnResult
	for attempt := 0; attempt <= a.cfg.SubagentMaxRestarts; attempt++ {
		if ctx.Err() != nil {
			return port.SpawnResult{Err: ctx.Err().Error()}
		}
		if attempt > 0 {
			a.emitAgentRestart(parent.ID, spec.Name, attempt, last.Err)
		}
		res, retry := a.runAttempt(ctx, parent, depth, spec, req)
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
		c, ok := a.sessions[req.ReuseSession]
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
		a.sessions[child.ID] = child
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

	msgID := "m_" + newID()
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: msgID,
		Parts:     []session.Part{{Kind: session.PartText, Text: req.Prompt}},
	})
	a.appendFact(ctx, child.ID, event.TypePromptSubmitted, actor, pd)
	a.touch(child.ID) // seed liveness so the watchdog doesn't fire immediately

	// Announce the spawn on the parent session for the live pane view.
	sd, _ := json.Marshal(event.AgentStatusData{
		AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name, State: "running",
	})
	a.publishTransient(parent.ID, event.TypeAgentSpawned, actor, sd)

	// Run the subagent with a hard per-attempt timeout. Register its cancel so
	// the user can interrupt this specific subagent (Esc on its focused pane).
	attemptCtx, cancel := context.WithTimeout(ctx, a.cfg.SubagentTimeout)
	defer cancel()
	a.mu.Lock()
	a.cancels[child.ID] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cancels, child.ID)
		a.mu.Unlock()
	}()

	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		t, e := a.runLoop(attemptCtx, child, spec, depth+1, 0, false)
		done <- outcome{t, e}
	}()

	announceDone := func() {
		d, _ := json.Marshal(event.AgentStatusData{
			AgentID: string(child.ID), Parent: string(parent.ID), Role: spec.Name, State: "done",
		})
		a.publishTransient(parent.ID, event.TypeAgentStatus, actor, d)
	}

	ticker := time.NewTicker(maxDur(a.cfg.SubagentStall/3, time.Second))
	defer ticker.Stop()
	for {
		select {
		case o := <-done:
			announceDone()
			if o.err != nil {
				// Retry only when our own per-attempt timeout fired (not a parent
				// cancellation, which should propagate as a terminal failure).
				timedOut := attemptCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
				return port.SpawnResult{Err: o.err.Error(), SessionID: child.ID}, timedOut
			}
			return port.SpawnResult{Text: o.text, SessionID: child.ID}, false

		case <-ticker.C:
			if a.idleFor(child.ID) > a.cfg.SubagentStall {
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
