package lua

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// hookedScriptTool is a plugin that knows nothing of any other plugin: it hands a script one
// function of its own, hears every call the script makes, and reports progress itself.
func hookedScriptTool(t *testing.T) port.Tool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, `name="hooked"
capabilities=["tool","call_tools"]
call_tools=["read","grep"]
`, `
magi.register_tool{ name = "script", description = "d",
  schema = { type = "object", properties = {} },
  execute = function(args)
    local seen = {}
    local out, err = magi.run_script(args.src, {
      globals = { mark = function(x) seen[#seen+1] = "mark:" .. tostring(x); return "ok:" .. tostring(x) end },
      on_call = function(e)
        local s = e.phase .. ":" .. e.tool .. ":" .. tostring(e.batch) .. ":" .. tostring(e.index) .. ":" .. tostring(e.size)
        if e.phase == "end" then s = s .. ":" .. tostring(e.ok) end
        seen[#seen+1] = s
      end,
    })
    magi.progress("one", { stages = { { id = "a", label = "g1", state = "running" } } })
    magi.progress("again", { stages = { { id = "a", label = "g1", state = "running" } } })
    magi.progress("two", { stages = { { id = "a", label = "g2", state = "done", done = 2, total = 2 } }, force = true })
    magi.progress("words only")
    table.sort(seen)
    return tostring(out) .. "|" .. table.concat(seen, ",") .. "|ERR:" .. tostring(err)
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

// A plugin gives a script functions of its own (globals) and hears each call the script makes
// (on_call): the host knows no plugin's words, it only hands over the facts.
func TestAPluginLendsAScriptItsFunctionsAndHearsItsCalls(t *testing.T) {
	var calls []recordedCall
	env := recordingEnv(&calls)
	// call_all's workers report k/n at the same time, so the recorders take a lock.
	var mu sync.Mutex
	var graphs, notes []string
	env.EmitStages = func(text string, st []event.ProgressStage) {
		mu.Lock()
		graphs = append(graphs, text+"="+st[0].Label+"/"+st[0].State)
		mu.Unlock()
	}
	env.EmitProgress = func(text string) { mu.Lock(); notes = append(notes, text); mu.Unlock() }
	got := runScript(t, hookedScriptTool(t), env, map[string]any{"src": `
      print(mark("x"))
      call_tool("read", {path="a"})
      call_all({ {"read"}, {"grep"} })`})
	for _, want := range []string{"ok:x\n|", "mark:x",
		"start:read:0:1:1", "end:read:0:1:1:true",
		"start:read:1:1:2", "end:read:1:1:2:true", "start:grep:1:2:2", "end:grep:1:2:2:false", "|ERR:nil"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Join(graphs, ",") != "one=g1/running,two=g2/done" {
		t.Errorf("unchanged stages go once and force skips the gap: %q", graphs)
	}
	if len(notes) == 0 || notes[len(notes)-1] != "words only" {
		t.Errorf("progress with no graph is a plain note: %q", notes)
	}
}
