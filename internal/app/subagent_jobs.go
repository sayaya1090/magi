package app

import (
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The register of children that are running or have just finished, so the screen can show one.
//
// A spawned child runs for minutes inside a single tool call. Its events go to its own session id
// and the TUI subscribes to exactly one, so without this the only sign of it is the parent's
// spinner and one clipped progress line. The pane strip that already exists for background commands
// is the right place to show it — it has the strip, the detail view and the side panel — and it is
// driven by polling a registry. This is that registry, in the same shape, so the strip gains a
// second producer rather than a second mechanism.
//
// Finished children are kept for a short while: the strip fades a pane out after it ends, and a
// child dropped the instant it returned would vanish mid-turn with nothing to read. The list is
// bounded because a loop-engineering plugin can start a great many of them in one turn, and a map
// that only grows is a defect this tree has already paid for once.
type SubagentJob struct {
	ID      string // the child's session id
	Tool    string // the tool that spawned it
	Task    string // what it was asked to do
	Started time.Time
	Running bool
	Ended   time.Time
	Steps   int
	Err     string // why it stopped, when it stopped badly
}

// A child's PROGRESS is not kept here. The pane reads the child's own session for what it did, and
// a copy of the same thing under a second lifetime would be one more pair to keep in step. What
// this register answers is the question the session cannot: which children exist right now, and
// which just ended — a session log does not know it is over.

// subagentJobKeep bounds the finished children retained, oldest dropped first.
const subagentJobKeep = 16

type subagentJobs struct {
	mu    sync.Mutex
	order []string // insertion order, so the oldest FINISHED one is the one dropped
	byID  map[string]*SubagentJob
}

func (r *subagentJobs) start(id session.SessionID, tool, task string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = map[string]*SubagentJob{}
	}
	sid := string(id)
	r.byID[sid] = &SubagentJob{ID: sid, Tool: tool, Task: task, Started: time.Now(), Running: true}
	r.order = append(r.order, sid)
	r.evictLocked()
}

func (r *subagentJobs) finish(id session.SessionID, steps int, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.byID[string(id)]
	if !ok {
		return
	}
	j.Running, j.Ended, j.Steps, j.Err = false, time.Now(), steps, errText
	r.evictLocked()
}

// evictLocked drops the oldest FINISHED children past the cap. A running one is never dropped: it
// is the thing the strip exists to show, and forgetting it would leave a pane that never ends.
func (r *subagentJobs) evictLocked() {
	done := 0
	for _, id := range r.order {
		if j, ok := r.byID[id]; ok && !j.Running {
			done++
		}
	}
	for i := 0; i < len(r.order) && done > subagentJobKeep; {
		id := r.order[i]
		j, ok := r.byID[id]
		if !ok {
			r.order = append(r.order[:i], r.order[i+1:]...)
			continue
		}
		if j.Running {
			i++
			continue
		}
		delete(r.byID, id)
		r.order = append(r.order[:i], r.order[i+1:]...)
		done--
	}
}

func (r *subagentJobs) list() []SubagentJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SubagentJob, 0, len(r.order))
	for _, id := range r.order {
		if j, ok := r.byID[id]; ok {
			out = append(out, *j)
		}
	}
	return out
}

// SubagentJobs returns the children running now and the ones that just finished, oldest first —
// what the pane strip polls, mirroring BackgroundJobs.
func (a *App) SubagentJobs() []SubagentJob { return a.subJobs.list() }
