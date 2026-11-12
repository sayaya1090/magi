//go:build !windows

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A cancelled command (a timeout, the person's stop, or magi shutting down) must end what the
// shell started, not just the shell. Before, the cancel killed the shell alone: its `sleep 30`
// stayed in the session group with init as its parent, and nothing — not the timeout, not the
// exit — ever ended it.
func TestCancellingACommandEndsTheProcessesItStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait")
	cmd.SysProcAttr = detachTTY(nil)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for child == 0 {
		if b, err := os.ReadFile(pidFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if time.Now().After(deadline) {
			t.Fatal("the shell never started its child")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() { _ = syscall.Kill(child, syscall.SIGKILL) }()

	if err := killCmdTree(cmd); err != nil {
		t.Fatalf("killCmdTree: %v", err)
	}
	_ = cmd.Wait()

	deadline = time.Now().Add(3 * time.Second)
	for syscall.Kill(child, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the shell's child %d is still running after the command was cancelled", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
