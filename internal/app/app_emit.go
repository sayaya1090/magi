package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

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

// emitDebate surfaces a disagreement-triggered rebuttal round (council debate) as a
// transient progress note, so the otherwise-internal rebuttal is observable in the
// TUI and headless stream. No-op when debate did not run (unanimous vote or disabled).
func (a *App) emitDebate(sid session.SessionID, actor event.Actor, phase string, round int, d *council.DebateOutcome) {
	if d == nil {
		return
	}
	verb := "held"
	if d.Before != d.After {
		verb = "flipped " + string(d.Before) + "→" + string(d.After)
	}
	a.emitToolProgress(sid, actor, "", "council",
		fmt.Sprintf("%s round %d debate: split → rebuttal, %d member(s) changed, %s",
			phase, round, d.Changed, verb))
}

// emitToolProgress publishes a long-running tool's live progress note as a
// transient (bus-only, droppable) event so the TUI and headless stream can show
// what is being waited on. No-op when the bus is absent.
func (a *App) emitToolProgress(sid session.SessionID, actor event.Actor, callID, name, text string) {
	d, _ := json.Marshal(event.ToolProgressData{CallID: callID, Name: name, Text: text})
	a.publishTransient(sid, event.TypeToolProgress, actor, d)
}

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

// deliberating reports whether the session is currently inside a side-LLM gate — the council
// (plan audit / contract / termination) or the planner — rather than executing tools. Those gates
// make sequential member/planner LLM calls that stream nothing and hold no tool in flight, so to
// the subagent lease they look idle even though they are ACTIVE work. The lease treats this like a
// tool in flight (a legitimately long, silent operation) and does not kill mid-gate; the council
// round cap and the attempt backstop still bound a genuinely stuck gate. This is what lets a worker
// run its own plan-audit without being killed for the silence between council rounds.
func (a *App) deliberating(sid session.SessionID) bool {
	s := a.currentStage(sid)
	return s == stageCouncil || s == stagePlan
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

// enterGen / leaveGen bracket one model generation for a session, and generating reports whether
// the session is inside one. It is the third silence, and the one nothing was watching.
//
// The lease judge is reached only when no deterministic test says the child is working, and the two
// that existed cover a tool executing (toolInFlight) and a council/planner side-call
// (deliberating). A child's OWN main-loop generation is neither: it runs in the execute stage with
// no tool in flight, and on a slow local model it is where most of the child's wall time goes. So
// the lease timer landed there, found every deterministic test false, asked the judge, and the
// judge killed a child that was mid-sentence — observed as four subagents whose last recorded event
// is the provider's own `context canceled`, one of them three seconds after a successful write.
//
// Same argument as toolInFlight's, different silence: a generation emits no events until the first
// token arrives, and it is bounded on its own (the stall watchdog re-issues a silent stream, the
// backstop still caps the attempt), so extending here cannot hold a runaway open.
func (a *App) enterGen(sid session.SessionID) {
	v, _ := a.genRunning.LoadOrStore(sid, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (a *App) leaveGen(sid session.SessionID) {
	if v, ok := a.genRunning.Load(sid); ok {
		v.(*atomic.Int64).Add(-1)
	}
}

func (a *App) generating(sid session.SessionID) bool {
	if v, ok := a.genRunning.Load(sid); ok {
		return v.(*atomic.Int64).Load() > 0
	}
	return false
}

// noteGenToken records that this session's current generation just produced output, and genFresh
// reports whether it did so recently enough to still count as producing.
//
// "In a generation" is not by itself evidence of work: a wedged backend holds an open stream that
// emits nothing, and extending a lease for that would keep exactly the runaway the cap exists to
// stop. Tokens arriving is the difference, and it is the same line the stream watchdog draws — a
// stream silent past streamStallTimeout is aborted and re-issued as hung. So the lease extends
// while output is flowing and hands a silent stream back to the judge.
func (a *App) noteGenToken(sid session.SessionID) { a.genLastToken.Store(sid, time.Now()) }

func (a *App) genFresh(sid session.SessionID) bool {
	v, ok := a.genLastToken.Load(sid)
	if !ok {
		return false // nothing has ever arrived on this session's stream
	}
	window := streamStallTimeout
	if window <= 0 {
		window = 2 * time.Minute
	}
	return time.Since(v.(time.Time)) < window
}

// bumpProductive records that this session just produced something a later step can build on: a
// file mutation, or an exercising command run for the first time this epoch. It is the lease's
// measure of PRODUCTIVITY, kept at the App level because the guard that knows these facts lives
// inside the child's own run loop and the lease supervisor runs in the parent.
func (a *App) bumpProductive(sid session.SessionID) {
	v, _ := a.productive.LoadOrStore(sid, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// productiveCount returns the running count for a session (0 when it has produced nothing).
func (a *App) productiveCount(sid session.SessionID) int64 {
	if v, ok := a.productive.Load(sid); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}
