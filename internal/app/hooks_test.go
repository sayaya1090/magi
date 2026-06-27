package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// newHookApp builds an App with a real OS platform (hooks shell out).
func newHookApp(t *testing.T, cfg Config) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, &fakeLLM{}, builtin.Default(), bus.New(), platform.New(), cfg)
}

func TestHookMatches(t *testing.T) {
	cases := []struct {
		h           HookSpec
		event, tool string
		want        bool
	}{
		{HookSpec{Event: "PreToolUse", Match: "bash"}, "PreToolUse", "bash", true},
		{HookSpec{Event: "PreToolUse", Match: "bash"}, "PreToolUse", "write", false},
		{HookSpec{Event: "pretooluse", Match: "*"}, "PreToolUse", "anything", true}, // case-insensitive event, wildcard
		{HookSpec{Event: "Stop", Match: ""}, "Stop", "", true},                      // empty match = any
		{HookSpec{Event: "PostToolUse", Match: "edit"}, "Stop", "edit", false},      // wrong event
	}
	for i, c := range cases {
		if got := c.h.matches(c.event, c.tool); got != c.want {
			t.Errorf("case %d: matches(%q,%q)=%v want %v", i, c.event, c.tool, got, c.want)
		}
	}
}

// A PreToolUse hook with a non-zero exit blocks the tool and its stderr is the feedback.
func TestPreToolHookBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks run via /bin/sh, which Windows lacks (POSIX-shell feature)")
	}
	a := newHookApp(t, Config{Hooks: []HookSpec{
		{Event: "PreToolUse", Match: "bash", Command: "echo nope >&2; exit 1"},
	}})
	block := a.runPreToolHooks(context.Background(), t.TempDir(), "bash", "")
	if block != "nope" {
		t.Fatalf("block = %q, want %q", block, "nope")
	}
	// A non-matching tool is not blocked.
	if b := a.runPreToolHooks(context.Background(), t.TempDir(), "read", ""); b != "" {
		t.Fatalf("read should not be blocked, got %q", b)
	}
}

// A zero-exit PreToolUse hook does not block.
func TestPreToolHookAllows(t *testing.T) {
	a := newHookApp(t, Config{Hooks: []HookSpec{
		{Event: "PreToolUse", Match: "*", Command: "exit 0"},
	}})
	if b := a.runPreToolHooks(context.Background(), t.TempDir(), "bash", ""); b != "" {
		t.Fatalf("want no block, got %q", b)
	}
}

// The built-in harness auto-formats Go files written through file-modifying tools.
func TestHarnessAutoFormatsGo(t *testing.T) {
	a := newHookApp(t, Config{Harness: true})
	wd := t.TempDir()
	unformatted := "package x\nfunc  F( ){\n}\n"
	path := filepath.Join(wd, "x.go")
	if err := os.WriteFile(path, []byte(unformatted), 0o644); err != nil {
		t.Fatal(err)
	}
	a.runPostToolHooks(context.Background(), wd, "write", path)
	got, _ := os.ReadFile(path)
	want := "package x\n\nfunc F() {\n}\n"
	if string(got) != want {
		t.Fatalf("autoformat gave %q, want %q", got, want)
	}
}

// Harness off → no auto-format.
func TestHarnessOffSkipsFormat(t *testing.T) {
	a := newHookApp(t, Config{Harness: false})
	wd := t.TempDir()
	src := "package x\nfunc  F( ){\n}\n"
	path := filepath.Join(wd, "x.go")
	os.WriteFile(path, []byte(src), 0o644)
	a.runPostToolHooks(context.Background(), wd, "write", path)
	got, _ := os.ReadFile(path)
	if string(got) != src {
		t.Fatalf("harness off should leave file untouched, got %q", got)
	}
}

// A Stop hook with a non-zero exit reports failure (forces the agent to continue).
func TestStopHookFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks run via /bin/sh, which Windows lacks (POSIX-shell feature)")
	}
	a := newHookApp(t, Config{Hooks: []HookSpec{
		{Event: "Stop", Command: "echo 'tests failed' >&2; exit 1"},
	}})
	if fail := a.runStopHooks(context.Background(), t.TempDir()); fail != "tests failed" {
		t.Fatalf("stop fail = %q, want %q", fail, "tests failed")
	}
}

// A passing Stop hook lets the turn finish.
func TestStopHookPasses(t *testing.T) {
	a := newHookApp(t, Config{Hooks: []HookSpec{
		{Event: "Stop", Command: "exit 0"},
	}})
	if fail := a.runStopHooks(context.Background(), t.TempDir()); fail != "" {
		t.Fatalf("want pass, got %q", fail)
	}
}

// Hooks receive the tool name and path via environment.
func TestHookEnv(t *testing.T) {
	a := newHookApp(t, Config{Hooks: []HookSpec{
		{Event: "PreToolUse", Match: "*", Command: `test "$MAGI_TOOL" = edit && test "$MAGI_PATH" = /tmp/f || (echo badenv >&2; exit 1)`},
	}})
	if b := a.runPreToolHooks(context.Background(), t.TempDir(), "edit", "/tmp/f"); b != "" {
		t.Fatalf("env not passed: %q", b)
	}
}
