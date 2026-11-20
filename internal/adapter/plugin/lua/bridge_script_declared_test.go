package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

func declaredScriptTool(t *testing.T, manifest string) port.Tool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, manifest, `
magi.register_tool{ name = "script", description = "d",
  schema = { type = "object", properties = {} },
  execute = function(args)
    local out, err = magi.run_script(args.src, {})
    return tostring(out) .. "|ERR:" .. tostring(err)
  end }
`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	tool, ok := sink.Get("script")
	if !ok {
		t.Fatal("no tool")
	}
	return tool
}

// Which tools a plugin's scripts may call is the plugin's to say (call_tools in plugin.toml), not a
// list in the host: a call to anything it did not declare is refused before it reaches the host.
func TestAScriptCallsOnlyWhatItsPluginDeclared(t *testing.T) {
	tool := declaredScriptTool(t, `name="declared"
capabilities=["tool","call_tools"]
call_tools=["read"]
`)
	var calls []recordedCall
	got := runScript(t, tool, recordingEnv(&calls), map[string]any{"src": `
      print(call_tool("read", {path="a"}))
      local v, err = call_tool("grep", {pattern="x"})
      print(tostring(v), err)
      local r = call_all({ {"grep"}, {"read"} })
      print(r[1].ok, r[1].error, r[2].ok)`})
	for _, c := range calls {
		if c.name != "read" {
			t.Fatalf("an undeclared %s reached the host: %q", c.name, got)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("want the two declared reads to reach the host, got %d: %q", len(calls), got)
	}
	if strings.Count(got, "grep is not declared in call_tools") != 2 {
		t.Fatalf("an undeclared call must be refused in words, both ways: %q", got)
	}
}

// A plugin that declares no call_tools can call nothing: opening tools is an act, not a default.
func TestAPluginThatDeclaresNoToolsCallsNone(t *testing.T) {
	tool := declaredScriptTool(t, `name="silent"
capabilities=["tool","call_tools"]
`)
	var calls []recordedCall
	got := runScript(t, tool, recordingEnv(&calls), map[string]any{"src": `print(call_tool("read", {path="a"}))`})
	if len(calls) != 0 || !strings.Contains(got, "read is not declared in call_tools") {
		t.Fatalf("calls %v, got %q", calls, got)
	}
}
