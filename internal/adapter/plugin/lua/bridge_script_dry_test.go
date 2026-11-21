package lua

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// optScriptTool runs args.src through magi.run_script with dry and serial taken from the call, and
// returns "<output>|ERR:<err>|CALLS:<json of the dry calls>".
func optScriptTool(t *testing.T) port.Tool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"optscripter\"\ncapabilities=[\"tool\",\"call_tools\"]\ncall_tools=[\"read\",\"grep\",\"glob\",\"bash\",\"write\"]\n", `
magi.register_tool{ name = "script", description = "d",
  schema = { type = "object", properties = {} },
  execute = function(args)
    local out, err, calls = magi.run_script(args.src, { timeout = 5, dry = args.dry, serial = args.serial })
    return tostring(out) .. "|ERR:" .. tostring(err) .. "|CALLS:" .. (calls and magi.json_encode(calls) or "nil")
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

// A dry run reaches no tool: every call — call_tool, call_all, call_graph — is recorded in the order
// the script makes it and answered with an empty text, and the recorded calls come back with the
// output. A call the plugin did not declare is still refused, and not recorded.
func TestRunScriptDryRecordsTheCallsAndRunsNone(t *testing.T) {
	var reached int32
	env := port.ToolEnv{ParallelToolSafe: func(string) bool { return true },
		CallTool: func(context.Context, string, json.RawMessage) (session.ToolResult, error) {
			atomic.AddInt32(&reached, 1)
			return session.ToolResult{Content: json.RawMessage(`"real"`)}, nil
		}}
	got := runScript(t, optScriptTool(t), env, map[string]any{"dry": true, "src": `
local a = call_tool("read", {path="a.md"})
print("read:" .. tostring(a))
call_all({ {"grep", {pattern="x"}}, {"bash", {command="echo hi"}} })
call_graph({ {id="g", tool="glob", args={pattern="*.go"}}, {id="w", tool="write", args={path="o"}, after={"g"}} })
local none, why = call_tool("edit", {})
print(tostring(none), why)`})
	if n := atomic.LoadInt32(&reached); n != 0 {
		t.Fatalf("a dry run reached the host %d times: %q", n, got)
	}
	parts := strings.SplitN(got, "|CALLS:", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "read:\n") || !strings.Contains(parts[0], "edit is not declared") || !strings.HasSuffix(parts[0], "|ERR:nil") {
		t.Fatalf("output: %q", got)
	}
	var calls []struct{ Tool, Args string }
	if err := json.Unmarshal([]byte(parts[1]), &calls); err != nil {
		t.Fatalf("calls %q: %v", parts[1], err)
	}
	want := []string{`read {"path":"a.md"}`, `grep {"pattern":"x"}`, `bash {"command":"echo hi"}`, `glob {"pattern":"*.go"}`, `write {"path":"o"}`}
	if len(calls) != len(want) {
		t.Fatalf("want %d recorded calls, got %+v", len(want), calls)
	}
	for i, c := range calls {
		if c.Tool+" "+c.Args != want[i] {
			t.Fatalf("call %d: got %q, want %q (all: %+v)", i, c.Tool+" "+c.Args, want[i], calls)
		}
	}
}

// serial = {...} names tools that never run alongside another call in this run, even when the host
// would let them overlap: a routine's writing calls go one at a time.
func TestRunScriptSerialToolsNeverOverlap(t *testing.T) {
	peakOf := func(args map[string]any) int32 {
		var active, peak int32
		env := port.ToolEnv{ParallelToolSafe: func(string) bool { return true },
			CallTool: func(_ context.Context, name string, _ json.RawMessage) (session.ToolResult, error) {
				n := atomic.AddInt32(&active, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
						break
					}
				}
				time.Sleep(80 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return session.ToolResult{Content: json.RawMessage(`"` + name + `"`)}, nil
			}}
		got := runScript(t, optScriptTool(t), env, args)
		if !strings.Contains(got, "|ERR:nil") {
			t.Fatalf("script failed: %q", got)
		}
		return atomic.LoadInt32(&peak)
	}
	all := `call_all({ {"bash", {command="a"}}, {"bash", {command="b"}}, {"read", {path="r"}} })`
	graph := `call_graph({ {id="a", tool="bash", args={command="a"}}, {id="b", tool="bash", args={command="b"}}, {id="r", tool="read", args={path="r"}} })`
	for name, src := range map[string]string{"call_all": all, "call_graph": graph} {
		if p := peakOf(map[string]any{"src": src}); p < 2 {
			t.Fatalf("%s: control run never overlapped (peak %d); the check cannot see an overlap", name, p)
		}
		if p := peakOf(map[string]any{"src": src, "serial": []string{"bash"}}); p != 1 {
			t.Fatalf("%s: with serial={bash}, calls overlapped (peak %d)", name, p)
		}
	}
}

// A dry run's record is in the order the script asked, also for a call_all the host would run all at
// once: the record is compared with a trace call by call, so a shuffled one would not match.
func TestRunScriptDryKeepsTheOrderOfACallAll(t *testing.T) {
	env := port.ToolEnv{ParallelToolSafe: func(string) bool { return true },
		CallTool: func(context.Context, string, json.RawMessage) (session.ToolResult, error) {
			t.Error("a dry run reached the host")
			return session.ToolResult{}, nil
		}}
	got := runScript(t, optScriptTool(t), env, map[string]any{"dry": true, "src": `
local calls = {}
for i = 1, 16 do calls[i] = {"read", {path = "f" .. i}} end
call_all(calls)`})
	parts := strings.SplitN(got, "|CALLS:", 2)
	var calls []struct{ Tool, Args string }
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &calls) != nil || len(calls) != 16 {
		t.Fatalf("record: %q", got)
	}
	for i, c := range calls {
		if want := `{"path":"f` + strconv.Itoa(i+1) + `"}`; c.Args != want {
			t.Fatalf("call %d recorded as %s, want %s: %+v", i+1, c.Args, want, calls)
		}
	}
}

// A dry run needs no tool call to hang off: it reaches no tool, so a command (outside any tool call)
// can check a script it is about to save. A real run still needs one (TestRunScriptNeedsAToolCall).
func TestRunScriptDryRunsOutsideAToolCall(t *testing.T) {
	got := runScript(t, optScriptTool(t), port.ToolEnv{}, map[string]any{"dry": true, "src": `call_tool("read", {path="a"}) print("ok")`})
	if got != `ok`+"\n|ERR:nil|CALLS:"+`[{"args":"{\"path\":\"a\"}","tool":"read"}]` {
		t.Fatalf("a dry run outside a tool call: %q", got)
	}
}
