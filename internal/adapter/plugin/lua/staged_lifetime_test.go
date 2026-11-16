package lua

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func stagedTool(t *testing.T, body string) *luaTool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"staged\"\ncapabilities=[\"tool\",\"spawn\"]\n", body)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	tool, ok := sink.Get("st")
	if !ok {
		t.Fatal("no st tool")
	}
	return tool.(*luaTool)
}

func runStaged(t *testing.T, tool *luaTool, env port.ToolEnv) (res session.ToolResult) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the staged tool panicked: %v", r)
		}
	}()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The child runs with the plugin's lock released, so the plugin can be unloaded or reloaded under
// it, which closes its Lua state. finish must not then run on a state that is gone: the call ends
// as a tool error saying why (review of 79813a83a: a real explore unloaded mid-child panicked).
func TestAStagedToolWhosePluginWentAwayEndsWithAnError(t *testing.T) {
	tool := stagedTool(t, `magi.register_tool{ name = "st", description = "d",
  prepare = function(args) return { prompt = "p" } end,
  finish = function(r) return r.text end }`)
	res := runStaged(t, tool, port.ToolEnv{Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		tool.plugin.close() // what an unload or reload does while the child runs
		return port.SpawnResult{Text: "child said"}, nil
	}})
	if got := resultString(t, res); !res.IsError || !strings.Contains(got, "unloaded") {
		t.Errorf("want a tool error naming the unload, got error=%v %q", res.IsError, got)
	}
}

// review is called from the child's goroutine. It must see THIS call's env, as prepare and finish do:
// a review that asks what the child did (magi.child_steps) is the reason review exists.
func TestAReviewOfAStagedToolSeesItsCall(t *testing.T) {
	tool := stagedTool(t, `local seen = "none"
magi.register_tool{ name = "st", description = "d",
  prepare = function(args)
    return { prompt = "p", review = function(round, text, steps, sid)
      local s, err = magi.child_steps(sid)
      seen = err or ("steps=" .. #s)
      return nil
    end }
  end,
  finish = function(r) return seen end }`)
	env := port.ToolEnv{
		ChildSteps: func(context.Context, string) ([]port.ChildStep, error) { return []port.ChildStep{{}}, nil },
	}
	env.Spawn = func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
		if sp.Review == nil {
			t.Fatal("review did not reach the spawn spec")
		}
		if _, err := sp.Review(1, "t", 1, "s_child"); err != nil {
			t.Fatalf("review: %v", err)
		}
		return port.SpawnResult{Text: "ok"}, nil
	}
	if got := resultString(t, runStaged(t, tool, env)); got != "steps=1" {
		t.Errorf("review did not see its call's env: %q", got)
	}
}
