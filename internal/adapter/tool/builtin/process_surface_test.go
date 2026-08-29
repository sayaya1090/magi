package builtin

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The two shutdown sweeps are safe on a machine with nothing running — the interactive-exit path
// must never fail because there was nothing to clean.
func TestKillSweepsAreSafeWhenIdle(t *testing.T) {
	bg.KillAll()
	KillBackgroundProcesses()
}

// startLSP refuses a server that is not installed, naming it — the caller's install advice hangs
// on that name.
func TestStartLSPNamesTheMissingServer(t *testing.T) {
	_, err := startLSP(context.Background(), lspServer{argv: []string{"no-such-lsp-xyz"}}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no-such-lsp-xyz") {
		t.Fatalf("the missing binary is the advice: %v", err)
	}
}

// readCmdline is a /proc read: words on Linux, the honest empty everywhere else.
func TestReadCmdlineIsProcBound(t *testing.T) {
	got := readCmdline(1)
	if runtime.GOOS != "linux" && got != "" {
		t.Fatalf("no /proc here, so no annotation: %q", got)
	}
}

// killOwner sends the named signal to exactly one pid — our own child here, which dies of it.
func TestKillOwnerSignalsOnePid(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err := killOwner(cmd.Process.Pid, "term"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done: // died of the signal
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the precise kill did not land")
	}
}

// drop removes exactly the server it was handed — a replacement that took the key meanwhile is
// not this caller's to evict — and closes the dropped one off-thread.
func TestLSPPoolDropEvictsByIdentity(t *testing.T) {
	mine := &warmLSP{cli: &lspClient{}}
	other := &warmLSP{cli: &lspClient{}}
	m := &lspPoolManager{warm: map[string]*warmLSP{"go": mine}}
	m.drop("go", other) // not the one under the key
	if m.warm["go"] != mine {
		t.Fatal("a mismatched pointer must not evict the current holder")
	}
	m.drop("go", mine)
	if _, held := m.warm["go"]; held {
		t.Fatal("the handed server leaves the pool")
	}
}
