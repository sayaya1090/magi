package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// parallelCaller runs its calls through ToolEnv.CallTool all at once, as call_all does.
type parallelCaller struct {
	got  []string
	errs []error
}

func (*parallelCaller) Name() string            { return "parallel_caller" }
func (*parallelCaller) Description() string     { return "runs tools together" }
func (*parallelCaller) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *parallelCaller) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Calls []struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"calls"`
	}
	_ = json.Unmarshal(raw, &in)
	c.got, c.errs = make([]string, len(in.Calls)), make([]error, len(in.Calls))
	var wg sync.WaitGroup
	for i, call := range in.Calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := env.CallTool(ctx, call.Name, call.Args)
			c.got[i], c.errs[i] = string(r.Content), err
		}()
	}
	wg.Wait()
	return session.ToolResult{Content: json.RawMessage(`"done"`)}, nil
}

// fakeChild stands in for a subagent tool (explore) and keeps the most that ran at once. Its
// metadata is explore's own (plugins/explore/init.lua: subagent, readonly_children).
type fakeChild struct{ running, peak int32 }

func (*fakeChild) Name() string            { return "explore" }
func (*fakeChild) Description() string     { return "child" }
func (*fakeChild) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*fakeChild) Meta() port.ToolMetadata {
	return port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}
}
func (c *fakeChild) Execute(_ context.Context, raw json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	n := atomic.AddInt32(&c.running, 1)
	for {
		m := atomic.LoadInt32(&c.peak)
		if n <= m || atomic.CompareAndSwapInt32(&c.peak, m, n) {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	atomic.AddInt32(&c.running, -1)
	b, _ := json.Marshal("child answered " + string(raw))
	return session.ToolResult{Content: b}, nil
}

// Calls a script runs together each get their own result back, recorded with their parent.
func TestCallsAScriptRunsTogetherEachGetTheirOwnResult(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	caller := &parallelCaller{}
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	calls := make([]map[string]any, 12)
	for i := range calls {
		dir := filepath.Join(s.Workdir, fmt.Sprintf("d%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("only-in-%d.txt", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		calls[i] = map[string]any{"name": "list", "args": map[string]any{"path": dir}}
	}
	raw, _ := json.Marshal(map[string]any{"calls": calls})
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		&session.ToolCall{CallID: "outer", Name: "parallel_caller", Args: raw}, newRunGuard(nil), "")
	for i := range calls {
		if caller.errs[i] != nil || !strings.Contains(caller.got[i], fmt.Sprintf("only-in-%d.txt", i)) {
			t.Fatalf("call %d got someone else's result: %q %v", i, caller.got[i], caller.errs[i])
		}
		for j := range calls {
			if j != i && strings.Contains(caller.got[i], fmt.Sprintf("only-in-%d.txt", j)) {
				t.Fatalf("call %d carries call %d's result", i, j)
			}
		}
	}
}

// A script cannot start more children at once than a step may: subagent calls wait for a slot.
func TestChildrenAScriptStartsTogetherWaitForASlot(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", MaxParallelChildren: 2})
	child := &fakeChild{}
	a.tools.Register(child)
	caller := &parallelCaller{}
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	calls := make([]map[string]any, 6)
	for i := range calls {
		calls[i] = map[string]any{"name": "explore", "args": map[string]any{"question": fmt.Sprintf("q%d", i)}}
	}
	raw, _ := json.Marshal(map[string]any{"calls": calls})
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		&session.ToolCall{CallID: "outer", Name: "parallel_caller", Args: raw}, newRunGuard(nil), "")
	for i := range calls {
		if caller.errs[i] != nil || !strings.Contains(caller.got[i], fmt.Sprintf("q%d", i)) {
			t.Fatalf("child %d: %q %v", i, caller.got[i], caller.errs[i])
		}
	}
	if p := atomic.LoadInt32(&child.peak); p > 2 || p < 2 {
		t.Fatalf("%d children ran at once; the limit is 2", p)
	}
}
