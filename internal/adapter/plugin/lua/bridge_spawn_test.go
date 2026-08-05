package lua

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// spawnPlugin loads a one-tool plugin whose Lua body is the given source.
func spawnPlugin(t *testing.T, body string) (*Host, port.Tool) {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"looper\"\ncapabilities=[\"tool\",\"spawn\"]\n", `
magi.register_tool{ name = "loop", description = "d",
  schema = { type = "object", properties = {} },
  execute = function(args) `+body+` end }
`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	tool, ok := sink.Get("loop")
	if !ok {
		t.Fatal("the plugin registered no tool")
	}
	return h, tool
}

// The tool call's context reaches the child.
//
// The child is bounded and registered for interrupt, but a bridge that handed it a fresh
// context.Background() severed it from the turn that started it: cancelling the parent left the
// child running until its own deadline, so Ctrl-C looked dead for as long as that took.
func TestTheToolCallsContextReachesTheChild(t *testing.T) {
	_, tool := spawnPlugin(t, `local r = magi.spawn{prompt = "go"} return r.text or ""`)

	var childCancelled bool
	env := port.ToolEnv{Spawn: func(ctx context.Context, _ port.SpawnSpec) (port.SpawnResult, error) {
		// Whatever the host hands down must carry the caller's cancellation. A detached context
		// reports nil here however long the parent has been dead.
		childCancelled = ctx.Err() != nil
		return port.SpawnResult{Text: "done"}, nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the parent turn is already over by the time the child would start
	if _, err := tool.Execute(ctx, json.RawMessage(`{}`), env); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !childCancelled {
		t.Error("the child was started on a context that outlives the tool call — cancelling the parent cannot reach it")
	}
}

// resultString pulls the text out of a tool result.
func resultString(t *testing.T, res session.ToolResult) string {
	t.Helper()
	var s string
	if json.Unmarshal(res.Content, &s) == nil {
		return s
	}
	return string(res.Content)
}
