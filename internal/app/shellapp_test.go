package app

import (
	"context"
	"os/exec"
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
	// Honor Dir: readForCheck passes the workspace there, and a double that drops it makes every
	// relative-path check read as unreadable — a check that works in production failing in tests, or
	// worse, a test passing because it exercised nothing.
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	out, err := cmd.CombinedOutput()
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
