package app

import (
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// What the run loop RECORDS about a live session, in one place. Write-only today: the lease
// supervisor that read these fields came out with the delegation machinery (2bd1fb68), and no
// consumer has replaced it — the fields are kept as the observation points a future supervisor
// will need, not as facts anything currently acts on.
//
// The original shape: several smaller questions in a row — is a tool executing, is a
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

// touch records activity for a session. Nothing reads lastEvent today (see the package comment).
func (a *App) touch(sid session.SessionID) { a.live(sid).lastEvent.Store(time.Now().UnixNano()) }

// enterTool / leaveTool bracket a single tool execution for a session. The toolInFlight reader
// this fed ("don't mistake a long silent bash build for a wedged child") went with the lease;
// the bracket stays because it is the one place tool-execution in-flight is observed.
func (a *App) enterTool(sid session.SessionID) { a.live(sid).tools.Add(1) }
func (a *App) leaveTool(sid session.SessionID) { a.live(sid).tools.Add(-1) }

// enterGen / leaveGen bracket one model generation for a session. History, kept because it names
// the failure a future supervisor must not repeat: the old lease judge could not see a child's own
// main-loop generation (no tool in flight, no side-call), found every deterministic test false, and
// killed children mid-sentence — observed as four subagents whose last recorded event was the
// provider's own `context canceled`, one three seconds after a successful write. Whatever reads
// these next must treat "inside a generation" as work.
func (a *App) enterGen(sid session.SessionID) { a.live(sid).gens.Add(1) }
func (a *App) leaveGen(sid session.SessionID) { a.live(sid).gens.Add(-1) }

// noteGenToken records that this session's current generation just produced output.
//
// Write-only since the lease machinery came out (2bd1fb68 removed the EXTEND/KILL judge that read
// this through genFresh): nothing consumes lastToken today. Kept because it is the one place "the
// model is actually emitting" is observed, which the next supervisor that needs the fact will want
// — but read the fields, not the old comments, before building on them: the earlier text here
// described a lease that no longer exists, and the silence bounds it referenced have since split
// (firstTokenBound for the pre-first-token wait, streamStallTimeout between tokens).
func (a *App) noteGenToken(sid session.SessionID) {
	a.live(sid).lastToken.Store(time.Now().UnixNano())
}

// bumpProductive records that this session just produced something a later step can build on: a
// file mutation, or an exercising command run for the first time this epoch. Recorded from inside
// the child's own run loop (the guard is what knows these facts); nothing reads `produced` today.
func (a *App) bumpProductive(sid session.SessionID) { a.live(sid).produced.Add(1) }
