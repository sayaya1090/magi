package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slowTool blocks for `sleep`, emitting no events, and records whether its
// context was cancelled (interrupted) before it slept fully. It models a silent,
// long-running tool (e.g. a multi-minute bash build) whose activity is invisible
// to the sidecar liveness check.
type slowTool struct {
	sleep       time.Duration
	calls       atomic.Int64
	interrupted atomic.Bool
}

func (s *slowTool) Name() string            { return "slow" }
func (s *slowTool) Description() string     { return "test tool that blocks silently" }
func (s *slowTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *slowTool) Execute(ctx context.Context, _ json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	s.calls.Add(1)
	select {
	case <-time.After(s.sleep):
	case <-ctx.Done():
		s.interrupted.Store(true)
	}
	return session.ToolResult{Content: mustJSON("slow done")}, nil
}

// A subagent blocked in a silent, long-running tool is NOT mistaken for a wedged
// child: the stall watchdog is suppressed while a tool is in flight. The tool
// runs to completion uninterrupted even though its silent duration exceeds
// SubagentStall (and crosses a watchdog tick), and the subagent then finishes on
// its FIRST attempt. Without the tool-in-flight gate the stall would fire mid-tool,
// cancel it, and force a restart.
func TestStallSuppressedWhileToolInFlight(t *testing.T) {
	st := &slowTool{sleep: 1300 * time.Millisecond}
	reg := builtin.Default()
	reg.Register(st)

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// call 0 → invoke the slow tool; call 1 (after its result) → final answer.
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("slow", `{}`),
		textStep("finished"),
	}}
	a := New(store, llm, reg, bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       200 * time.Millisecond, // short: the silent tool far outlasts it
		SubagentTimeout:     30 * time.Second,       // generous: must not be what ends the attempt
		SubagentMaxRestarts: 2,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err != "" {
		t.Fatalf("expected clean completion, got error: %s", res.Err)
	}
	if !strings.Contains(res.Text, "finished") {
		t.Fatalf("expected the final answer, got %q", res.Text)
	}
	if st.interrupted.Load() {
		t.Fatal("slow tool was cancelled mid-run — the stall watchdog fired despite a tool being in flight")
	}
	if n := st.calls.Load(); n != 1 {
		t.Fatalf("expected the tool to run exactly once (no restart), ran %d times", n)
	}
}
