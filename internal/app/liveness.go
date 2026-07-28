package app

import (
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// What the supervisors ask about a running session, in one place.
//
// The lease supervisor and the stall watchdog both answer one question — is this session working
// right now — and they answer it by asking several smaller ones in a row: is a tool executing, is a
// generation in flight, did tokens arrive recently, has it produced anything, when did anything at
// all last happen. Each of those used to live in its own sync.Map keyed by session id, added one at
// a time as each supervisor learned a new way to be wrong, and the cost of that shape was paid
// three ways:
//
//   - Every signal invented its own meaning for ABSENT. A missing tool count means "no tool", a
//     missing token time means "nothing has ever arrived", a missing activity time meant "idle for
//     zero seconds" — three different readings of the same empty slot, each correct only where it
//     was written and none of them written down.
//   - Nothing ever deleted an entry. A run spawns a session per subagent attempt, so five maps grew
//     one entry each per attempt and kept them for the life of the process. Nobody noticed, because
//     no single place owned the set.
//   - Adding the next signal meant another map, another LoadOrStore, another zero-value convention.
//
// So it is one struct behind one map, and the zero value is the answer for a session nothing has
// happened in yet: no tool, no generation, no token, nothing produced, never seen. Counters are
// atomic rather than mutex-guarded because the readers are supervisors sampling from another
// goroutine while the session runs — they want a current value, not a consistent snapshot across
// fields.
type sessionLiveness struct {
	lastEvent atomic.Int64 // unix nanos of the last recorded event (0 = nothing yet)
	lastToken atomic.Int64 // unix nanos of the last streamed token (0 = the stream has produced nothing)
	tools     atomic.Int64 // tool executions in flight
	gens      atomic.Int64 // model generations in flight
	produced  atomic.Int64 // mutations + first-seen exercising commands
}

// live returns the liveness record for a session, creating it on first use.
func (a *App) live(sid session.SessionID) *sessionLiveness {
	if v, ok := a.liveness.Load(sid); ok {
		return v.(*sessionLiveness)
	}
	v, _ := a.liveness.LoadOrStore(sid, &sessionLiveness{})
	return v.(*sessionLiveness)
}

// touch records activity for a session (used by the sidecar liveness check).
func (a *App) touch(sid session.SessionID) { a.live(sid).lastEvent.Store(time.Now().UnixNano()) }

// enterTool / leaveTool bracket a single tool execution for a session, and toolInFlight reports
// whether any tool is currently running. The stall watchdog consults toolInFlight so a legitimately
// long, silent tool (e.g. a multi-minute bash build that emits no events until it returns) is not
// mistaken for a wedged child. A tool that hangs past its own timeout is still bounded by the hard
// cap.
func (a *App) enterTool(sid session.SessionID) { a.live(sid).tools.Add(1) }
func (a *App) leaveTool(sid session.SessionID) { a.live(sid).tools.Add(-1) }

// enterGen / leaveGen bracket one model generation for a session, and generating reports whether the
// session is inside one. It is the third silence, and the one nothing was watching.
//
// The lease judge is reached only when no deterministic test says the child is working, and the two
// that existed cover a tool executing (toolInFlight) and a council/planner side-call
// (deliberating). A child's OWN main-loop generation is neither: it runs in the execute stage with
// no tool in flight, and on a slow local model it is where most of the child's wall time goes. So
// the lease timer landed there, found every deterministic test false, asked the judge, and the
// judge killed a child that was mid-sentence — observed as four subagents whose last recorded event
// is the provider's own `context canceled`, one of them three seconds after a successful write.
func (a *App) enterGen(sid session.SessionID) { a.live(sid).gens.Add(1) }
func (a *App) leaveGen(sid session.SessionID) { a.live(sid).gens.Add(-1) }

// noteGenToken records that this session's current generation just produced output, and genFresh
// reports whether it did so recently enough to still count as producing.
//
// "In a generation" is not by itself evidence of work: a wedged backend holds an open stream that
// emits nothing, and extending a lease for that would keep exactly the runaway the cap exists to
// stop. Tokens arriving is the difference, and it is the same line the stream watchdog draws — a
// stream silent past streamStallTimeout is aborted and re-issued as hung. So the lease extends
// while output is flowing and hands a silent stream back to the judge.
func (a *App) noteGenToken(sid session.SessionID) {
	a.live(sid).lastToken.Store(time.Now().UnixNano())
}

// bumpProductive records that this session just produced something a later step can build on: a
// file mutation, or an exercising command run for the first time this epoch. It is the lease's
// measure of PRODUCTIVITY, kept here because the guard that knows these facts lives inside the
// child's own run loop while the lease supervisor runs in the parent.
func (a *App) bumpProductive(sid session.SessionID) { a.live(sid).produced.Add(1) }
