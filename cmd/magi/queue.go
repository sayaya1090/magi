package main

import (
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Work a companion has taken and not started yet.
//
// # Why a queue and not a refusal
//
// A busy companion used to refuse, and the asker was told to try later or ask somebody else. That
// is not how work is handed to somebody who is at their desk: it goes in their inbox. Refusing
// also puts the retry on the asker, which means either a model that gives up on the right
// companion or one that polls — and polling is the blocking call this whole thing exists to avoid.
//
// It became possible with the side sessions. The old refusal had two reasons and both were about
// there being one conversation: a request sent into a running turn is re-read BY that turn, and
// "the answer is the next turn that finishes" is only unambiguous if no turn was open. Neither
// survives a conversation of its own.
//
// # Taken is not started, and the difference is said
//
// The receipt is minted when the work is TAKEN, so the asker has a handle immediately and the
// answer cannot be lost between accepting and starting. Until it starts, asking about it says so —
// queued, and how many are in front. An asker that only ever heard silence could not tell a
// companion that is thinking from one that has not begun.
//
// # There is a limit, and being at it is the signal
//
// An unbounded queue is a companion accepting work it will never get to, which is worse than a
// refusal because it looks like acceptance. So it refuses past a point — and the refusals are the
// thing worth counting: a companion that is repeatedly full is the evidence for running more than
// one of it.

// maxWaiting is how much work a companion will hold before it starts refusing.
//
// Small on purpose. The queue exists so a request does not bounce off somebody who happens to be
// mid-turn; it is not a backlog to be worked through overnight. Past a handful the honest answer
// is that this companion is not the one to ask, and the asker has a roster in front of it.
const maxWaiting = 4

// drainEvery is the backstop. Starting the next piece is nudged whenever something is taken or a
// turn this started has ended, so the tick is only for the case nothing here can observe: the
// person's own turn finishing in their own session.
const drainEvery = 3 * time.Second

// drainSoon is the wait after a try that found the workspace still busy with work waiting, and
// quickTries is how many of those in a row before falling back to the backstop.
//
// A turn ending and the workspace being free are not the same instant. The nudge rides the bus
// event; the flag it then reads is dropped by the run goroutine afterwards. Lose that race and the
// piece behind it sat for the whole backstop with everything it needed already true — three
// seconds of a companion looking like it was thinking, once per handover, for no reason.
//
// Bounded on purpose: a person's own turn can hold the workspace for an hour, and a retry that
// never gave up would be a poll for the length of it. A second of trying is far more than state
// cleanup needs, and the backstop is still there for everything else.
const (
	drainSoon  = 100 * time.Millisecond
	quickTries = 10
)

// pending is one piece of taken work, waiting for the workspace.
type pending struct {
	receipt string
	session session.SessionID
	text    string
	// looking marks a piece the asker declared a question. Its session may only read, so it does
	// not wait for the workspace — see startNext. Carried on the item because the decision about
	// what it must wait for is made when it reaches the head, not when it arrives.
	looking bool
}

// waiting is the queue itself. A pointer type, like the receipts beside it: the engine holding it
// is copied by value into the socket's serve loop, and two copies with two queues would be work
// accepted by one and drained by neither.
type waiting struct {
	mu    sync.Mutex
	items []pending
	// stopped is why a receipt will never answer, for the few that cannot be started at all.
	// Without it a failed start is a receipt pointing at a session where nothing ever happens,
	// which the asking side cannot tell from a companion still thinking.
	stopped map[string]string
	nudge   chan struct{}
	// inHand is whether a piece taken from here is running right now.
	//
	// Not derivable from the queue, which this piece has already left, and not from the state a
	// dashboard shows, which is read off the session a person attaches to. Without it a companion
	// working through somebody's request looks exactly like one with nothing to do.
	// inHand counts pieces running right now — a COUNT, not a flag: a looking piece runs
	// beside a writing one by design, and when this was a bool whichever piece ended first
	// cleared it, advertising a companion mid-change as free.
	inHand int
	// annMu makes "read the state, publish the state" one atomic act. The reads were under
	// w.mu and the publish outside it (the callback writes the published record and must not
	// run under the state lock), so two concurrent ended()s could publish in the wrong order
	// and leave a stale "busy" as the FINAL record about an idle companion — the exact
	// stuck-busy the ended() comment says nothing would ever clear. Ordered publishes make
	// the last word match the state.
	annMu sync.Mutex
	// changed says what this companion is carrying, whenever that changes. It is how both numbers
	// reach the published record and from there the roster somebody chooses from — pushed, because
	// nothing outside this process can see a queue held in memory.
	changed func(waiting int, handling bool)
}

func newWaiting(changed func(int, bool)) *waiting {
	return &waiting{stopped: map[string]string{}, nudge: make(chan struct{}, 1), changed: changed}
}

// announce says what is being carried, outside the lock: whatever it reaches writes a file, and
// holding a mutex across that would put the disk between one asker and the next.
func (w *waiting) announce(n int, handling bool) {
	if w.changed != nil {
		w.changed(n, handling)
	}
}

// began and ended bracket a piece actually running.
//
// ended is called on every way out of watching a piece, including the ways that mean this daemon
// has LOST track of it. That is deliberate: saying "free" while busy costs an asker one wait, and
// the refusal still protects the workspace, whereas a flag stuck at "busy" would push every asker
// away from a companion that is fine and nothing would ever clear it.
func (w *waiting) began() {
	w.annMu.Lock()
	defer w.annMu.Unlock()
	w.mu.Lock()
	w.inHand++
	depth := len(w.items)
	w.mu.Unlock()
	w.announce(depth, true)
}

func (w *waiting) ended() {
	w.annMu.Lock()
	defer w.annMu.Unlock()
	w.mu.Lock()
	w.inHand--
	if w.inHand < 0 {
		w.inHand = 0 // ended runs on lost-track paths too; a stray extra call must not wedge the count
	}
	depth, hand := len(w.items), w.inHand > 0
	w.mu.Unlock()
	w.announce(depth, hand)
}

// list is what is waiting, oldest first — the order it will be started in.
//
// The label rather than the receipt: a reader wants to know WHO is waiting on this companion, and
// the receipt is a handle for the asker, not a name for anybody else.
func (w *waiting) list() []pending {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]pending(nil), w.items...)
}

// take puts work in the queue, or says the queue is full.
func (w *waiting) take(p pending) (ahead int, ok bool) {
	w.mu.Lock()
	if len(w.items) >= maxWaiting {
		w.mu.Unlock()
		return len(w.items), false
	}
	w.items = append(w.items, p)
	ahead = len(w.items) - 1
	w.mu.Unlock()
	w.annMu.Lock()
	w.mu.Lock()
	depth, hand := len(w.items), w.inHand > 0
	w.mu.Unlock()
	w.announce(depth, hand)
	w.annMu.Unlock()
	w.wake()
	return ahead, true
}

// peek is the head of the queue without taking it.
//
// What a piece has to wait for depends on what it IS — a question waits for nothing, a change
// waits for the workspace — and that cannot be decided before looking at it. Taking it to look
// would put a question that arrived second in front of a change that arrived first every time the
// workspace was busy, which is a queue that reorders itself under load.
func (w *waiting) peek() (pending, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.items) == 0 {
		return pending{}, false
	}
	return w.items[0], true
}

// next takes the head of the queue, if there is one.
func (w *waiting) next() (pending, bool) {
	w.mu.Lock()
	if len(w.items) == 0 {
		w.mu.Unlock()
		return pending{}, false
	}
	p := w.items[0]
	w.items = w.items[1:]
	w.mu.Unlock()
	w.annMu.Lock()
	w.mu.Lock()
	depth, hand := len(w.items), w.inHand > 0
	w.mu.Unlock()
	w.announce(depth, hand)
	w.annMu.Unlock()
	return p, true
}

// where reports a receipt's place in the queue, counting from one, and whether it is in it at all.
func (w *waiting) where(receipt string) (place, total int, queued bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, p := range w.items {
		if p.receipt == receipt {
			return i + 1, len(w.items), true
		}
	}
	return 0, len(w.items), false
}

// givenUp is why a receipt will never answer, if it will not.
func (w *waiting) givenUp(receipt string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	why, ok := w.stopped[receipt]
	return why, ok
}

func (w *waiting) giveUp(receipt, why string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped[receipt] = why
}

// wake asks the drain to look. Never blocks: one pending wake-up is as good as two, because the
// drain re-reads the whole queue when it runs.
func (w *waiting) wake() {
	select {
	case w.nudge <- struct{}{}:
	default:
	}
}
