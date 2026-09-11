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
//
// ⚠ **Skipped where killOwner cannot be asked at all.** port_owner is withdrawn on platforms where
// neither /proc nor lsof can answer (see registry.go and withheldHere), and killOwner is that
// tool's other half — on Windows it returns "port_owner is not supported on this platform", which
// this test then reported as the precise kill failing. It is a deliberate absence, not a defect,
// and the `sleep 30` below is not a command that exists there either.
//
// portOwnerSupported rather than a GOOS check, because it is the same constant the registration
// branch reads: one fact, one spelling.
func TestKillOwnerSignalsOnePid(t *testing.T) {
	if !portOwnerSupported {
		t.Skip("이 플랫폼은 port_owner 를 내주지 않는다 — killOwner 는 그 도구의 다른 반쪽이다")
	}
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
	// The fake needs a closable stdin: drop closes the evicted server on a goroutine, and a nil
	// pipe there is a panic that only fires when the scheduler feels like it — measured as a
	// full-suite crash after three green single runs.
	mine := &warmLSP{cli: &lspClient{in: nopWC{}}}
	other := &warmLSP{cli: &lspClient{in: nopWC{}}}
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

type nopWC struct{}

func (nopWC) Write(p []byte) (int, error) { return len(p), nil }
func (nopWC) Close() error                { return nil }
