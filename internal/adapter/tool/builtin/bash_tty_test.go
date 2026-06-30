package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// TestDetachTTYSetsSession asserts the mechanism is actually wired: on Unix detachTTY
// must set Setsid (the behavioral test below can't prove this in a headless/CI env, where
// /dev/tty reads fail fast even without the fix — this white-box check guards the regression).
func TestDetachTTYSetsSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("detachTTY is a deliberate no-op on Windows")
	}
	if a := detachTTY(nil); a == nil || !a.Setsid {
		t.Fatal("detachTTY must return a SysProcAttr with Setsid=true on Unix")
	}
}

// TestBashDetachesControllingTTY: a command that reads from /dev/tty must fail fast
// (no controlling terminal) rather than hang until the timeout. Without detachTTY's
// Setsid, on a host that HAS a controlling terminal this read would block for the full
// timeout; this test asserts it returns well under that.
func TestBashDetachesControllingTTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/tty / controlling-terminal detach on Windows")
	}
	done := make(chan struct{})
	var res = struct{ took time.Duration }{}
	start := time.Now()
	go func() {
		// timeout 10s; if the tty read hangs we'd approach that, so assert << 10s.
		_, _ = Bash{}.Execute(context.Background(),
			json.RawMessage(`{"command":"read line < /dev/tty; echo done","timeout":10}`),
			port.ToolEnv{Workdir: t.TempDir()})
		res.took = time.Since(start)
		close(done)
	}()
	select {
	case <-done:
		if res.took > 5*time.Second {
			t.Fatalf("reading /dev/tty should fail fast without a controlling terminal, took %v", res.took)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("bash hung reading /dev/tty — controlling terminal was not detached")
	}
}

// TestBashStillRunsNormally: detaching the tty must not break ordinary commands.
func TestBashStillRunsNormally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh echo semantics")
	}
	res, err := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"echo hello-magi"}`),
		port.ToolEnv{Workdir: t.TempDir()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("echo should succeed, got error result: %s", res.Content)
	}
	if !strings.Contains(string(res.Content), "hello-magi") {
		t.Fatalf("output missing, got: %s", res.Content)
	}
}
