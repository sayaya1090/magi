package lua

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A plugin's tool runs holding its plugin's lock. A call that comes back into the same plugin
// further down the same chain — here A's tool runs B's, whose script calls A again — would wait
// on that lock forever, past every script time limit. It is refused at once instead.
func TestACallBackIntoAPluginAlreadyRunningIsRefusedNotWaitedOn(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	for _, p := range []struct{ manifest, init string }{
		{"name=\"plug_a\"\ncapabilities=[\"tool\",\"call_tools\"]\ncall_tools=[\"b_run\"]\n", `
magi.register_tool{ name = "a_peek", description = "d", schema = { type = "object", properties = {} },
  execute = function(args) return "peeked" end }
magi.register_tool{ name = "a_run", description = "d", schema = { type = "object", properties = {} },
  execute = function(args)
    local out, err = magi.run_script('print(call_tool("b_run", {}))')
    return tostring(out) .. "|ERR:" .. tostring(err)
  end }
`},
		{"name=\"plug_b\"\ncapabilities=[\"tool\",\"call_tools\"]\ncall_tools=[\"a_peek\"]\n", `
magi.register_tool{ name = "b_run", description = "d", schema = { type = "object", properties = {} },
  execute = function(args)
    local out, err = magi.run_script('local r, why = call_tool("a_peek", {}) print(tostring(r), why)')
    return tostring(out) .. "|ERR:" .. tostring(err)
  end }
`},
	} {
		if _, err := h.Load(context.Background(), writePlugin(t, p.manifest, p.init)); err != nil {
			t.Fatalf("load: %v", err)
		}
	}
	var env port.ToolEnv
	env.CallTool = func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		tl, ok := sink.Get(name)
		if !ok {
			return session.ToolResult{}, errors.New("no tool " + name)
		}
		return tl.Execute(ctx, args, env)
	}
	run, _ := sink.Get("a_run")
	done := make(chan string, 1)
	go func() {
		res, err := run.Execute(context.Background(), json.RawMessage(`{}`), env)
		if err != nil {
			done <- "ERR " + err.Error()
			return
		}
		done <- string(res.Content)
	}()
	select {
	case got := <-done:
		if !strings.Contains(got, "plug_a") || !strings.Contains(got, "already running") {
			t.Fatalf("the call back into plug_a must be refused, naming it: %s", got)
		}
		if strings.Contains(got, "peeked") {
			t.Fatalf("the call back ran: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("A -> B -> A waited on A's lock (still waiting after 5s)")
	}
}
