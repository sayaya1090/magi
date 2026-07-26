package app

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/port"
)

// shellPlatform runs each command for real, so a test that needs a genuine process — a probe that
// connects, a `cat` of a file the test wrote — exercises the actual thing instead of a mock's idea
// of it, and records every argv the platform received.
type shellPlatform struct {
	mu   sync.Mutex
	cmds []string // every command as the platform received it
}

func (p *shellPlatform) Exec(ctx context.Context, c port.Cmd) (port.ExecResult, error) {
	p.mu.Lock()
	p.cmds = append(p.cmds, strings.Join(c.Args, " "))
	p.mu.Unlock()
	out, err := exec.CommandContext(ctx, c.Path, c.Args...).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return port.ExecResult{Stdout: out, ExitCode: 1}, nil
	}
	return port.ExecResult{Stdout: out, ExitCode: code}, nil
}
func (p *shellPlatform) ConfigDir() string           { return "" }
func (p *shellPlatform) DataDir() string             { return "" }
func (p *shellPlatform) TerminalCaps() port.TermCaps { return port.TermCaps{} }
func (p *shellPlatform) ProcessCPUTime(int) (time.Duration, bool) {
	return 0, false
}

func newShellApp(t *testing.T, plat port.Platform) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, &gateLLM{text: "ok"}, builtin.Default(), bus.New(), plat, Config{Permission: "allow"})
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this exercises POSIX tools (sh, cat, python3) that the Windows path does not use")
	}
}

// 127 is "command not found" and 126 is "found but not executable" — and 126 is now also what the
// typed runner returns for a check it cannot evaluate. All of them say nothing about the artifact,
// so all of them must read as "the check could not run" rather than as a failing deliverable.
func TestCheckUnrunnable(t *testing.T) {
	for _, code := range []int{126, 127} {
		if !checkUnrunnable(code) {
			t.Errorf("exit %d must count as the CHECK failing to run", code)
		}
	}
	for _, code := range []int{-1, 0, 1, 2} {
		if checkUnrunnable(code) {
			t.Errorf("exit %d must NOT be treated as unrunnable", code)
		}
	}
}

// The operator's own configured verify command is not a model-authored check: it is supposed to be
// able to build, and it reaches the shell verbatim. Nothing wraps or rewrites it.
func TestWorkflowVerifyCommandRunsVerbatim(t *testing.T) {
	skipOnWindows(t)
	plat := &shellPlatform{}
	a := newShellApp(t, plat)
	if _, code := a.runVerifyCmd(context.Background(), t.TempDir(), "echo built"); code != 0 {
		t.Fatalf("verify command exit = %d", code)
	}
	plat.mu.Lock()
	defer plat.mu.Unlock()
	if len(plat.cmds) != 1 || !strings.Contains(plat.cmds[0], "echo built") {
		t.Errorf("runVerifyCmd must hand the operator's command through unchanged, got %v", plat.cmds)
	}
}

// The audit's segmentation must read a command the way the SHELL does: a metacharacter inside a
// quoted argument is data, and a substitution really does open a new command position. Both were
// observed to matter live — a quote-blind split found a `make` inside `grep -q 'a\|make world'`.
func TestShellCommandSegmentsIsQuoteAware(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want []string
	}{
		{`grep -q 'build\|make world\|x' f`, []string{`grep -q 'build\|make world\|x' f`}},
		{`a > out; b`, []string{"a > out", "b"}},
		{`echo "$(make -s x)"`, []string{`echo "`, "make -s x", `"`}},
		{`echo 'a;b'`, []string{`echo 'a;b'`}},
	} {
		got := shellCommandSegments(tc.cmd)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("shellCommandSegments(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestShellWordAndEnvAssignment(t *testing.T) {
	if got := shellWord(`"log.txt"`); got != "log.txt" {
		t.Errorf("a fully quoted word must unquote, got %q", got)
	}
	if got := shellWord(`rm"`); got != `rm"` { // a fragment left by splitting inside a quote stays unrecognized
		t.Errorf("a half-quoted fragment must stay as-is, got %q", got)
	}
	if !isEnvAssignment("LC_ALL=C") {
		t.Error("LC_ALL=C is a leading env assignment, not the command word")
	}
	for _, f := range []string{"=x", "make", "g++", "a-b=c"} {
		if isEnvAssignment(f) {
			t.Errorf("%q must not be read as an env assignment", f)
		}
	}
}
