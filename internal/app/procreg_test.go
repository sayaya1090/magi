package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// cpuStubPlatform returns scripted per-pid CPU times so childProcActive can be
// tested deterministically on any host (the real /proc/GetProcessTimes counter is
// exercised by process_cputime_test.go). advance[pid] is added to the reported CPU
// on each successive sample, so a pid with advance >= procActiveMinCPU reads as
// actively working and one with advance 0 reads as idle. dead[pid] reports dead.
type cpuStubPlatform struct {
	mu      sync.Mutex
	base    map[int]time.Duration
	advance map[int]time.Duration
	dead    map[int]bool
	calls   map[int]int
}

func (p *cpuStubPlatform) ProcessCPUTime(pid int) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead[pid] {
		return 0, false
	}
	n := p.calls[pid]
	p.calls[pid]++
	return p.base[pid] + time.Duration(n)*p.advance[pid], true
}

func (p *cpuStubPlatform) Exec(context.Context, port.Cmd) (port.ExecResult, error) {
	return port.ExecResult{}, nil
}
func (p *cpuStubPlatform) ConfigDir() string           { return "" }
func (p *cpuStubPlatform) DataDir() string             { return "" }
func (p *cpuStubPlatform) TerminalCaps() port.TermCaps { return port.TermCaps{} }

func TestProcRegisterUnregister(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s1")
	if got := a.ownedPids(sid); got != nil {
		t.Fatalf("empty session should own no pids, got %v", got)
	}
	a.registerProc(sid, 100)
	a.registerProc(sid, 200)
	a.registerProc(sid, 100) // idempotent
	if got := a.ownedPids(sid); len(got) != 2 {
		t.Fatalf("want 2 owned pids, got %v", got)
	}
	a.unregisterProc(sid, 100)
	if got := a.ownedPids(sid); len(got) != 1 || got[0] != 200 {
		t.Fatalf("want only pid 200 after unregister, got %v", got)
	}
	a.unregisterProc(sid, 200)
	if got := a.ownedPids(sid); got != nil {
		t.Fatalf("want no pids after unregister-all, got %v", got)
	}
	// tracker closure dispatches register/unregister and ignores bad pids.
	track := a.procTracker(sid)
	track(0, true)  // ignored
	track(-5, true) // ignored
	track(300, true)
	if got := a.ownedPids(sid); len(got) != 1 || got[0] != 300 {
		t.Fatalf("tracker(true) should register 300, got %v", got)
	}
	track(300, false)
	if got := a.ownedPids(sid); got != nil {
		t.Fatalf("tracker(false) should drop 300, got %v", got)
	}
}

func TestChildProcActive(t *testing.T) {
	sid := session.SessionID("s1")

	t.Run("no pids", func(t *testing.T) {
		a := &App{plat: &cpuStubPlatform{}}
		if a.childProcActive(sid) {
			t.Error("no owned pids must not be active")
		}
	})

	t.Run("active pid extends", func(t *testing.T) {
		a := &App{plat: &cpuStubPlatform{
			base:    map[int]time.Duration{100: 0},
			advance: map[int]time.Duration{100: procActiveMinCPU + 10*time.Millisecond},
			calls:   map[int]int{},
		}}
		a.registerProc(sid, 100)
		if !a.childProcActive(sid) {
			t.Error("a CPU-advancing pid must read as active")
		}
	})

	t.Run("idle pid does not extend", func(t *testing.T) {
		a := &App{plat: &cpuStubPlatform{
			base:    map[int]time.Duration{100: 5 * time.Second}, // has history but flat now
			advance: map[int]time.Duration{100: 0},
			calls:   map[int]int{},
		}}
		a.registerProc(sid, 100)
		if a.childProcActive(sid) {
			t.Error("a flat-CPU (idle/wedged) pid must not extend the lease")
		}
	})

	t.Run("dead pid is pruned and inactive", func(t *testing.T) {
		a := &App{plat: &cpuStubPlatform{
			dead:  map[int]bool{100: true},
			calls: map[int]int{},
		}}
		a.registerProc(sid, 100)
		if a.childProcActive(sid) {
			t.Error("a dead pid must not be active")
		}
		if got := a.ownedPids(sid); got != nil {
			t.Errorf("dead pid should have been pruned, still own %v", got)
		}
	})

	t.Run("one active among idle extends", func(t *testing.T) {
		a := &App{plat: &cpuStubPlatform{
			base:    map[int]time.Duration{100: 0, 200: 0},
			advance: map[int]time.Duration{100: 0, 200: procActiveMinCPU + 5*time.Millisecond},
			calls:   map[int]int{},
		}}
		a.registerProc(sid, 100)
		a.registerProc(sid, 200)
		if !a.childProcActive(sid) {
			t.Error("one actively-working pid among idle ones must extend")
		}
	})

	t.Run("nil platform is safe", func(t *testing.T) {
		a := &App{}
		a.registerProc(sid, 100)
		if a.childProcActive(sid) {
			t.Error("nil platform must not report active")
		}
	})
}

// The A/B flag defaults ON and flips off only for an explicit "0".
func TestSubagentProcLeaseFlag(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_PROC_LEASE", "")
	if !subagentProcLeaseEnabled() {
		t.Error("default (unset) must be ON")
	}
	t.Setenv("MAGI_SUBAGENT_PROC_LEASE", "0")
	if subagentProcLeaseEnabled() {
		t.Error("=0 must disable")
	}
	t.Setenv("MAGI_SUBAGENT_PROC_LEASE", "1")
	if !subagentProcLeaseEnabled() {
		t.Error("=1 must be ON")
	}
}
