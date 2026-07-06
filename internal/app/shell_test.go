package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// TestRunShell covers the `!`-inline-shell backend: it runs a command in the
// workdir and returns combined output + the real exit code, with no permission gate.
func TestRunShell(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{})
	dir := t.TempDir()

	out, exit, err := a.RunShell(context.Background(), dir, "echo hi")
	if err != nil {
		t.Fatalf("RunShell echo: %v", err)
	}
	if exit != 0 || strings.TrimSpace(out) != "hi" {
		t.Fatalf("echo: got out=%q exit=%d, want \"hi\" / 0", out, exit)
	}

	// stderr is folded into the combined output, and a non-zero exit is surfaced.
	out, exit, err = a.RunShell(context.Background(), dir, "echo oops >&2; exit 3")
	if err != nil {
		t.Fatalf("RunShell exit3: %v", err)
	}
	if exit != 3 {
		t.Fatalf("exit: got %d, want 3", exit)
	}
	if !strings.Contains(out, "oops") {
		t.Fatalf("stderr not captured in combined output: %q", out)
	}
}

// TestRunShellNoPlatform: a nil platform is a clean error, not a panic.
func TestRunShellNoPlatform(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), nil, Config{})
	if _, _, err := a.RunShell(context.Background(), t.TempDir(), "echo hi"); err == nil {
		t.Fatal("expected error with nil platform")
	}
}
