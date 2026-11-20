package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// A plugin's tool runs holding its plugin's lock, so a script it runs could never get a call to
// another tool of the same plugin through: that call would wait on the lock forever, past the
// script's own time limit. Declaring one of its own tools in call_tools is refused at load.
func TestAPluginMayNotOpenItsOwnToolsToItsScripts(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"selfcaller\"\ncapabilities=[\"tool\",\"call_tools\"]\ncall_tools=[\"read\",\"helper\"]\n", `
magi.register_tool{ name = "helper", description = "d", schema = { type = "object", properties = {} },
  execute = function(args) return "helped" end }
magi.register_tool{ name = "script", description = "d", schema = { type = "object", properties = {} },
  execute = function(args) return tostring(magi.run_script("print(call_tool('helper', {}))")) end }
`)
	_, err := h.Load(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "helper") || !strings.Contains(err.Error(), "own") {
		t.Fatalf("want a load error naming its own tool helper, got %v", err)
	}
	if _, ok := sink.Get("script"); ok {
		t.Fatal("a refused plugin's tools were registered")
	}
}
