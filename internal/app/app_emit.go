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
