package app

import (
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// procSet is the set of live magi-managed background OS pids owned by one session.
type procSet struct {
	mu   sync.Mutex
	pids map[int]struct{}
}

// procTracker returns the TrackProc callback for a session's ToolEnv: it records a
// background job's pid while it runs and drops it on exit, so childProcActive can
// sample the CPU of a subagent's off-tool background work at lease expiry. Only
// bash{background:true} jobs (bgproc) call this; foreground work is already covered
// by toolInFlight, and the interject aside path has no execution tools (nil env).
func (a *App) procTracker(sid session.SessionID) func(pid int, running bool) {
	return func(pid int, running bool) {
		if pid <= 0 {
			return
		}
		if running {
			a.registerProc(sid, pid)
		} else {
			a.unregisterProc(sid, pid)
		}
	}
}

func (a *App) registerProc(sid session.SessionID, pid int) {
	v, _ := a.sessionProcs.LoadOrStore(sid, &procSet{pids: map[int]struct{}{}})
	ps := v.(*procSet)
	ps.mu.Lock()
	ps.pids[pid] = struct{}{}
	ps.mu.Unlock()
}

func (a *App) unregisterProc(sid session.SessionID, pid int) {
	v, ok := a.sessionProcs.Load(sid)
	if !ok {
		return
	}
	ps := v.(*procSet)
	ps.mu.Lock()
	delete(ps.pids, pid)
	ps.mu.Unlock()
}

// ownedPids snapshots the pids currently registered for a session.
func (a *App) ownedPids(sid session.SessionID) []int {
	v, ok := a.sessionProcs.Load(sid)
	if !ok {
		return nil
	}
	ps := v.(*procSet)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.pids) == 0 {
		return nil
	}
	out := make([]int, 0, len(ps.pids))
	for pid := range ps.pids {
		out = append(out, pid)
	}
	return out
}

// procActiveWindow is how long childProcActive samples CPU across; procActiveMinCPU
// is the minimum CPU advance in that window that counts as "actively working"
// (~5% of one core over 400ms). Below it, a live-but-idle or wedged process does
// NOT extend the lease — the judge decides, so a stuck background server is never
// kept alive forever.
const (
	procActiveWindow = 400 * time.Millisecond
	procActiveMinCPU = 20 * time.Millisecond
)

// childProcActive reports whether the child owns a background process that is
// actively burning CPU. It samples each owned pid's cumulative CPU time, waits
// procActiveWindow, and re-samples; any live pid whose CPU advanced past the
// threshold makes it true. Dead pids are pruned as they are noticed. Called only
// at lease expiry (rare), so the blocking sample is off the hot path.
func (a *App) childProcActive(sid session.SessionID) bool {
	if a.plat == nil {
		return false
	}
	pids := a.ownedPids(sid)
	if len(pids) == 0 {
		return false
	}
	type sample struct {
		cpu   time.Duration
		alive bool
	}
	first := make(map[int]sample, len(pids))
	anyAlive := false
	for _, pid := range pids {
		cpu, alive := a.plat.ProcessCPUTime(pid)
		first[pid] = sample{cpu, alive}
		if alive {
			anyAlive = true
		} else {
			a.unregisterProc(sid, pid) // reap the dead
		}
	}
	if !anyAlive {
		return false
	}
	time.Sleep(procActiveWindow)
	for pid, f := range first {
		if !f.alive {
			continue
		}
		cpu, alive := a.plat.ProcessCPUTime(pid)
		if !alive {
			a.unregisterProc(sid, pid)
			continue
		}
		if cpu-f.cpu >= procActiveMinCPU {
			return true
		}
	}
	return false
}
