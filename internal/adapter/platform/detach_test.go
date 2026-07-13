package platform

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A captured-output command that tries to read /dev/tty must fail fast (no
// controlling terminal) rather than seize the terminal or hang until timeout —
// this is what keeps `!ssh host` password prompts from corrupting the TUI.
func TestExecDetachesControllingTTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/tty / controlling-terminal detach on Windows")
	}
	done := make(chan struct{})
	var took time.Duration
	start := time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = OS{}.Exec(ctx, port.Cmd{Path: "/bin/sh", Args: []string{"-c", "read line < /dev/tty; echo done"}})
		took = time.Since(start)
		close(done)
	}()
	select {
	case <-done:
		if took > 4*time.Second {
			t.Fatalf("reading /dev/tty should fail fast without a controlling terminal, took %v", took)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Exec hung reading /dev/tty — controlling terminal was not detached")
	}
}

// Detaching the tty must not break ordinary captured-output commands.
func TestExecStillRunsNormally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh echo semantics")
	}
	res, err := OS{}.Exec(context.Background(), port.Cmd{Path: "/bin/sh", Args: []string{"-c", "echo hi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 || string(res.Stdout) != "hi\n" {
		t.Fatalf("echo should succeed, got exit=%d out=%q", res.ExitCode, res.Stdout)
	}
}
