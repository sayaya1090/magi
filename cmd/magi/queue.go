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

// pending is one piece of taken work, waiting for the workspace.
type pending struct {
	receipt string
	session session.SessionID
	text    string
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
	// changed says how much is waiting, whenever that changes. It is how the number reaches the
	// published record and from there the roster somebody chooses from — pushed, because nothing
	// outside this process can see a queue held in memory.
	changed func(int)
}

func newWaiting(changed func(int)) *waiting {
	return &waiting{stopped: map[string]string{}, nudge: make(chan struct{}, 1), changed: changed}
}

// announce says the new depth, outside the lock: whatever it reaches writes a file, and holding a
// mutex across that would put the disk between one asker and the next.
func (w *waiting) announce(n int) {
	if w.changed != nil {
		w.changed(n)
	}
}

// take puts work in the queue, or says the queue is full.
func (w *waiting) take(p pending) (ahead int, ok bool) {
	w.mu.Lock()
	if len(w.items) >= maxWaiting {
		w.mu.Unlock()
		return len(w.items), false
	}
	w.items = append(w.items, p)
	ahead, depth := len(w.items)-1, len(w.items)
	w.mu.Unlock()
	w.announce(depth)
	w.wake()
	return ahead, true
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
	depth := len(w.items)
	w.mu.Unlock()
	w.announce(depth)
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
