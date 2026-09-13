package idebridge

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
)

// How long a subscription waits for the REPLAY to end, and how long changes are gathered before
// being sent.
//
// Vars, not knobs: nothing outside the tests assigns them. A test that waited the real seconds would
// be a test nobody runs, and the batch interval below is the one number that decides what a live
// surface costs.
var (
	// rowsReplay bounds the wait for the end-of-replay frame. The live phase after it is deliberately
	// unbounded — a quiet conversation is the ordinary case for a tail — but before it a person is
	// waiting for a first picture, and a companion that accepted and then went quiet must produce a
	// sentence rather than a subscription that never answers.
	rowsReplay = 20 * time.Second
	// rowsBatch is how long arriving events are gathered before the difference is sent.
	//
	// ⚠ **This is the cost bound, and it is a measured decision rather than a preference.** Folding a
	// conversation is proportional to its length — 8ms at 1000 events, 157ms at 20000 (2026-09-13) —
	// and the difference between two folds costs about the same again. Sending per EVENT would pay
	// that per chunk of a streamed answer; sending per interval pays it at most once per interval,
	// and events that arrive while a fold is in progress simply join the next one. 80ms is faster
	// than a person perceives as lag and slow enough that a long conversation keeps up.
	rowsBatch = 80 * time.Millisecond
)

// rowsSub is one live subscription: a connection of its own, and a way to stop it.
type rowsSub struct {
	id      int
	conn    *daemon.Client
	stop    chan struct{}
	stopped sync.Once
}

func (s *rowsSub) end() {
	s.stopped.Do(func() {
		close(s.stop)
		// ⚠ Closing the connection is what unblocks the reader. It is parked in a socket read that no
		// flag can interrupt, so a subscription that only set a flag would go on holding a connection
		// and a goroutine until the process exited — and a panel that switches conversations often
		// would leak one per switch.
		_ = s.conn.Close()
	})
}

// rowsLive answers a conversation and then keeps answering it.
//
// The first frame is exactly what the one-shot door answers — the fold of the whole log — so a client
// implements one drawing path and uses it for both. After that it gets differences (live.go), which
// is the only way to keep a screen current without resending the conversation per streamed chunk.
//
// # Why the reply waits for the replay
//
// A subscription whose first frame is a difference would be a difference against nothing. So this
// answers when the replay ends, with the same patience the one-shot door has, and the frames follow.
func (b *bridge) rowsLive(req request) {
	if strings.TrimSpace(req.Session) == "" {
		b.fail(req.ID, "the rows method needs a session: {\"method\":\"rows\",\"session\":\"s_…\",\"live\":true}")
		return
	}
	c, err := daemon.DialWithin(b.socket, rowsConnect, rowsAsk)
	if err != nil {
		b.fail(req.ID, err.Error())
		return
	}
	peer, err := c.Hello()
	if err != nil {
		c.Close()
		b.fail(req.ID, err.Error())
		return
	}
	// Same gate as the one-shot door, for the same reason: a companion that cannot say where the
	// replay ends cannot be followed, and the failure without this is a subscription that never
	// sends its first frame.
	if !slices.Contains(peer.Caps, "history") {
		c.Close()
		b.fail(req.ID, "this companion streams a transcript but cannot say where the replay ends "+
			"(no \"history\" capability) — it is an older build than this bridge; restart it onto "+
			"this binary and live rows work")
		return
	}

	sub := &rowsSub{conn: c, stop: make(chan struct{})}
	b.smu.Lock()
	b.nextSub++
	sub.id = b.nextSub
	if b.subs == nil {
		b.subs = map[int]*rowsSub{}
	}
	b.subs[sub.id] = sub
	b.smu.Unlock()

	frames := make(chan liveFrame, 256)
	go func() {
		err := c.Follow(req.Session, 0, daemon.Tail{
			Each: func(e event.Event) bool {
				select {
				case frames <- liveFrame{event: &e}:
					return true
				case <-sub.stop:
					return false
				}
			},
			CaughtUp: func() {
				select {
				case frames <- liveFrame{caughtUp: true}:
				case <-sub.stop:
				}
			},
		})
		// The reason the stream ended travels with the last frame. A subscription that just stops is
		// indistinguishable from a quiet conversation, and a screen would go on claiming to be live.
		why := ""
		if err != nil {
			why = err.Error()
		}
		select {
		case frames <- liveFrame{done: true, why: why}:
		case <-sub.stop:
		}
		close(frames)
	}()
	go b.pump(req, sub, frames)
}

// liveFrame is what the reader hands the sender: an event, the end of the replay, or the end.
type liveFrame struct {
	event    *event.Event
	caughtUp bool
	done     bool
	why      string
}

// pump turns a stream of events into a reply and then into differences.
func (b *bridge) pump(req request, sub *rowsSub, frames <-chan liveFrame) {
	defer func() {
		b.smu.Lock()
		delete(b.subs, sub.id)
		b.smu.Unlock()
		sub.end()
	}()

	var (
		events []event.Event
		sent   []Row
		live   bool
		dirty  bool
		replay = time.NewTimer(rowsReplay)
		gather = time.NewTicker(rowsBatch)
		// ⚠ **Every way out of this loop says the subscription is over, and says it once.**
		//
		// There are three ways out and they race: the reader hands over its error, the client asks to
		// stop, and the reader's channel simply closes. Measured 2026-09-13 under a full parallel
		// suite: `stop` closes both the stop channel AND the connection, so the reader's own
		// "the stream ended" frame loses its race against the closed stop channel and is never
		// pushed — and the branch that noticed the closed channel returned in silence. The client had
		// its `ok` for the stop and then waited for ever for the `done` that never came, which is the
		// asymmetric lie this door has a test about: a screen that is told when something ENDS but
		// not when it broke keeps its last sentence for ever.
		// Said once because every caller RETURNS right after it — structural, not a flag. A flag was
		// written here first and then taken out: nothing could reach a second call, so it was a guard
		// with no branch to guard, and a guard nothing can exercise is worse than none (it reads as
		// proof that a case is handled).
		sayDone = func(why string) {
			if live {
				b.done(sub.id, why)
			}
		}
		flushed = func() {
			rows := Rows(events)
			if ops := Diff(sent, rows); len(ops) > 0 {
				b.reply(map[string]any{"sub": sub.id, "ops": ops})
				sent = rows
			}
			dirty = false
		}
	)
	defer replay.Stop()
	defer gather.Stop()

	for {
		select {
		case f, ok := <-frames:
			if !ok {
				sayDone("")
				return
			}
			switch {
			case f.done:
				if live {
					if dirty {
						flushed()
					}
					sayDone(f.why)
					return
				}
				// It ended before the first picture was complete. The client is still waiting on its
				// request, so the answer is a failure to THAT — not a frame about a subscription it
				// was never told it had.
				why := f.why
				if why == "" {
					why = "the transcript stream ended before the replay was over"
				}
				b.fail(req.ID, why)
				return
			case f.caughtUp:
				live = true
				replay.Stop()
				rows := Rows(events)
				if rows == nil {
					rows = []Row{}
				}
				sent = rows
				b.reply(map[string]any{"id": req.ID, "ok": true, "sub": sub.id,
					"rows": rows, "events": len(events)})
			default:
				events = append(events, *f.event)
				// Before the replay ends there is nothing to be a difference FROM, and the events are
				// what the first frame is built out of.
				if live {
					dirty = true
				}
			}
		case <-gather.C:
			if live && dirty {
				flushed()
			}
		case <-replay.C:
			if !live {
				b.fail(req.ID, "this companion has not said where the replay ends after "+
					rowsReplay.String()+" — it accepted the connection and went quiet")
				return
			}
		case <-sub.stop:
			sayDone("")
			return
		}
	}
}

// done says a subscription is over, and why when there is a why.
func (b *bridge) done(sub int, why string) {
	frame := map[string]any{"sub": sub, "done": true}
	if why != "" {
		frame["why"] = why
	}
	b.reply(frame)
}

// stop ends a subscription a client no longer wants.
//
// ⚠ **Without it a panel leaks a connection per conversation it looks at.** Each subscription holds
// its own socket (the stream is given to whoever keeps reading), so switching conversations without
// this would pile them up until the editor closed — and the daemon would go on shaping frames for
// screens nobody is looking at.
func (b *bridge) stopSub(req request) {
	if req.Sub <= 0 {
		b.fail(req.ID, "the stop method needs a subscription: {\"method\":\"stop\",\"sub\":1}")
		return
	}
	b.smu.Lock()
	sub := b.subs[req.Sub]
	b.smu.Unlock()
	if sub == nil {
		// Said, not silently accepted: a client that stopped the wrong number would otherwise think
		// it had stopped the right one and go on drawing frames it believes are finished.
		b.fail(req.ID, "no live subscription numbered "+itoa(float64(req.Sub))+" — it may have "+
			"already ended, and a frame with \"done\" says when that happened")
		return
	}
	sub.end()
	b.reply(map[string]any{"id": req.ID, "ok": true, "sub": req.Sub})
}

// endSubs stops every subscription. Called when the bridge itself is going away.
func (b *bridge) endSubs() {
	b.smu.Lock()
	subs := make([]*rowsSub, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.smu.Unlock()
	for _, s := range subs {
		s.end()
	}
}
