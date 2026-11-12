//go:build !windows

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = detachTTY(nil)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = killGroup(cmd.Process.Pid); _ = cmd.Wait() })
	return cmd
}

func exitsWithin(cmd *exec.Cmd, d time.Duration) bool {
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// A process killed outright (SIGKILL, a crash) runs no cleanup, and a Setsid child outlives it.
// The next start must find what the dead one left running and end it — but only the processes it
// really started: a recorded pid now held by something else is left alone.
func TestAnOrphanedChildIsReapedAtTheNextStart(t *testing.T) {
	dir := t.TempDir()
	if err := SetChildLedger(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetChildLedger("") })
	cmd := startSleeper(t)
	noteChild(cmd.Process.Pid) // never released: the process "crashed" here
	SetChildLedger("")

	reaped, unsettled := ReapOrphans(dir)
	if len(reaped) != 1 || reaped[0] != cmd.Process.Pid || len(unsettled) != 0 {
		t.Fatalf("want the orphan %d reaped and nothing unsettled, got %v %v", cmd.Process.Pid, reaped, unsettled)
	}
	if !exitsWithin(cmd, 5*time.Second) {
		t.Fatal("the orphaned child is still running after the reap")
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("the ledger should be empty after the reap, has %d", len(left))
	}
}

func TestARecycledPidIsNotKilled(t *testing.T) {
	dir := t.TempDir()
	cmd := startSleeper(t)
	// The ledger names this pid, but with a start time that is not this process's: the pid was
	// reused by something magi never started.
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(cmd.Process.Pid)), []byte("1.000001"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reaped, _ := ReapOrphans(dir); len(reaped) != 0 {
		t.Fatalf("a pid with a different start time must not be killed, reaped %v", reaped)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("the unrelated process was killed: %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatal("a stale entry should be dropped from the ledger")
	}
}

func TestAReleasedChildLeavesNoEntry(t *testing.T) {
	dir := t.TempDir()
	SetChildLedger(dir)
	t.Cleanup(func() { SetChildLedger("") })
	cmd := startSleeper(t)
	release := noteChild(cmd.Process.Pid)
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Fatalf("a running child should be recorded, ledger has %d", len(left))
	}
	release()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("a released child should leave no entry, ledger has %d", len(left))
	}
}
