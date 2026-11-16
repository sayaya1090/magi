package lua

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

var readersOnly = map[string]bool{"read": true, "grep": true, "glob": true, "list": true}

func loadExplore(t *testing.T) port.Tool {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "plugins", "explore")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Fatalf("bundled explore plugin not present: %v", err)
	}
	reg := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: reg, Runtime: RuntimeInfo{Workdir: t.TempDir()}})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load explore: %v", err)
	}
	tool, ok := reg.Get("explore")
	if !ok {
		t.Fatal("the explore plugin registered no explore tool")
	}
	return tool
}

// Reading a lot is the one kind of work a child can take off the main context cheaply and safely:
// what it read stays in the child, only its findings come back, and two children that can only look
// cannot collide, so several explore calls in one step run at once (loop.go, ReadOnlyChildren).
// Coding stays in the main conversation — the hand-off would cost more than it saves.
func TestExploreIsAReadOnlySubagentThatRunsInParallel(t *testing.T) {
	tool := loadExplore(t)
	m := port.ToolMetaOf(tool)
	if !m.Subagent || !m.ReadOnlyChildren {
		t.Fatalf("explore must be a subagent whose children only read: %+v", m)
	}
	d := tool.Description()
	for _, want := range []string{"one or two files", "directly", "several explore calls in one step", "in parallel"} {
		if !strings.Contains(d, want) {
			t.Errorf("the description does not carry the rule (%q):\n%s", want, d)
		}
	}
}

func TestExploreSpawnsAReaderAndReturnsItsFindings(t *testing.T) {
	tool := loadExplore(t)
	var got port.SpawnSpec
	env := port.ToolEnv{Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
		got = sp
		return port.SpawnResult{Text: "compact.go:242 takes the lock"}, nil
	}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"question":"where is the compaction lock taken?"}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.IsError || !strings.Contains(resultString(t, res), "compact.go:242 takes the lock") {
		t.Errorf("the child's findings did not come back: %+v %q", res.IsError, resultString(t, res))
	}
	if len(got.Tools) == 0 {
		t.Fatal("the child's tools must be named — an empty list is the whole set, which writes")
	}
	for _, n := range got.Tools {
		if !readersOnly[n] {
			t.Errorf("the child got %q, which is not a reader", n)
		}
	}
	if !strings.Contains(got.Prompt, "where is the compaction lock taken?") {
		t.Errorf("the question did not reach the child: %q", got.Prompt)
	}
	if strings.TrimSpace(got.System) == "" {
		t.Error("the child has no instructions")
	}

	fail := port.ToolEnv{Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		return port.SpawnResult{Text: "partial", Err: "backend down"}, nil
	}}
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"question":"q"}`), fail)
	if err == nil && (!res.IsError || !strings.Contains(resultString(t, res), "backend down")) {
		t.Errorf("a failed child must be reported as a failure with its reason, even beside partial text: %q", resultString(t, res))
	}
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"question":"  "}`), env)
	if err == nil && !res.IsError {
		t.Error("an empty question must be refused")
	}
}

func loadExploreWith(t *testing.T, section map[string]any) port.Tool {
	t.Helper()
	reg := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: reg, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()},
		PluginConfigs: map[string]map[string]any{"explore": section}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "explore")); err != nil {
		t.Fatalf("load explore: %v", err)
	}
	tool, ok := reg.Get("explore")
	if !ok {
		t.Fatal("no explore tool")
	}
	return tool
}

// How deep the reader digs is the caller's call (quick lookup vs a flow across files); the child
// is lean; and [plugins.explore] model routes it to a cheaper model when set.
func TestExploreDepthModelAndLean(t *testing.T) {
	specOf := func(tool port.Tool, args string) port.SpawnSpec {
		t.Helper()
		var got port.SpawnSpec
		env := port.ToolEnv{Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
			got = sp
			return port.SpawnResult{Text: "ok"}, nil
		}}
		if _, err := tool.Execute(context.Background(), json.RawMessage(args), env); err != nil {
			t.Fatal(err)
		}
		return got
	}
	plain := loadExploreWith(t, nil)
	for _, c := range []struct {
		args  string
		steps int
		secs  int
	}{{`{"question":"q"}`, 24, 300}, {`{"question":"q","depth":"quick"}`, 8, 120}, {`{"question":"q","depth":"thorough"}`, 40, 600}} {
		sp := specOf(plain, c.args)
		if sp.MaxSteps != c.steps || int(sp.Timeout.Seconds()) != c.secs {
			t.Errorf("%s: steps=%d timeout=%v, want %d/%ds", c.args, sp.MaxSteps, sp.Timeout, c.steps, c.secs)
		}
		if !sp.Lean {
			t.Errorf("%s: the reader child must be lean", c.args)
		}
		if sp.Model != "" {
			t.Errorf("no model configured, so the parent's model: got %q", sp.Model)
		}
	}
	if d := plain.Description(); !strings.Contains(d, "quick") || !strings.Contains(d, "thorough") {
		t.Errorf("the description must say when to pick a depth:\n%s", d)
	}
	cheap := loadExploreWith(t, map[string]any{"model": "haiku"})
	if sp := specOf(cheap, `{"question":"q"}`); sp.Model != "haiku" {
		t.Errorf("[plugins.explore] model did not reach the child: %q", sp.Model)
	}
}
