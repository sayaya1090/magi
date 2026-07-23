//go:build !windows

package platform

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// A live child reports alive=true; once reaped, its pid reports alive=false. On
// Linux the real /proc counter also lets us prove a busy child accrues CPU while a
// sleeping one does not — the signal the lease gate keys on. On darwin/BSD the
// counter is a liveness-only stub (returns 0 CPU), so the CPU-delta assertion is
// Linux-gated; the portable childProcActive logic is covered by procreg_test.go
// with a stub platform instead.
func TestProcessCPUTimeLivenessAndBusy(t *testing.T) {
	busy := exec.Command("sh", "-c", "while :; do :; done")
	if err := busy.Start(); err != nil {
		t.Fatalf("start busy child: %v", err)
	}
	defer func() {
		_ = busy.Process.Kill()
		_, _ = busy.Process.Wait()
	}()
	pid := busy.Process.Pid

	if _, alive := (OS{}).ProcessCPUTime(pid); !alive {
		t.Fatalf("running child pid %d reported not alive", pid)
	}
	if _, alive := (OS{}).ProcessCPUTime(0); alive {
		t.Error("pid 0 must report not alive")
	}
	if _, alive := (OS{}).ProcessCPUTime(-1); alive {
		t.Error("negative pid must report not alive")
	}

	if runtime.GOOS == "linux" {
		c0, _ := (OS{}).ProcessCPUTime(pid)
		time.Sleep(300 * time.Millisecond)
		c1, _ := (OS{}).ProcessCPUTime(pid)
		if c1-c0 <= 0 {
			t.Errorf("busy child CPU did not advance: %v -> %v", c0, c1)
		}

		idle := exec.Command("sleep", "5")
		if err := idle.Start(); err != nil {
			t.Fatalf("start idle child: %v", err)
		}
		defer func() { _ = idle.Process.Kill(); _, _ = idle.Process.Wait() }()
		i0, _ := (OS{}).ProcessCPUTime(idle.Process.Pid)
		time.Sleep(300 * time.Millisecond)
		i1, _ := (OS{}).ProcessCPUTime(idle.Process.Pid)
		if i1-i0 >= procHalfCore {
			t.Errorf("sleeping child unexpectedly burned CPU: %v -> %v", i0, i1)
		}
	}
}

// procHalfCore is a generous per-window CPU bound a genuinely idle process stays
// well under; only used to sanity-check the idle case on Linux.
const procHalfCore = 150 * time.Millisecond

// A pid that has exited reports not alive (the /proc entry / handle is gone).
func TestProcessCPUTimeDeadPid(t *testing.T) {
	c := exec.Command("sh", "-c", "exit 0")
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := c.Process.Pid
	_ = c.Wait() // reap so the pid is gone
	if _, alive := (OS{}).ProcessCPUTime(pid); alive {
		t.Errorf("reaped pid %d still reported alive", pid)
	}
}
