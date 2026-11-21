package lua

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// scriptTool loads a plugin whose one tool runs args.src through magi.run_script and returns
// "<output>|ERR:<err>".
func scriptTool(t *testing.T) port.Tool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"scripter\"\ncapabilities=[\"tool\",\"call_tools\"]\ncall_tools=[\"read\",\"grep\",\"glob\",\"list\",\"write\"]\n", `
magi.register_tool{ name = "script", description = "d",
  schema = { type = "object", properties = {} },
  execute = function(args)
    local out, err = magi.run_script(args.src, { timeout = args.timeout, output = args.output })
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

func runScript(t *testing.T, tool port.Tool, env port.ToolEnv, args map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := tool.Execute(context.Background(), raw, env)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	_ = json.Unmarshal(res.Content, &got)
	return got
}

type recordedCall struct {
	name string
	args string
}

// recordingEnv keeps every call; call_all makes them at the same time, so it takes a lock.
func recordingEnv(calls *[]recordedCall) port.ToolEnv {
	var mu sync.Mutex
	return port.ToolEnv{CallTool: func(_ context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		mu.Lock()
		*calls = append(*calls, recordedCall{name, string(args)})
		mu.Unlock()
		switch name {
		case "read":
			return session.ToolResult{Content: json.RawMessage(`"hello from read"`)}, nil
		case "grep":
			return session.ToolResult{Content: json.RawMessage(`"no such pattern"`), IsError: true}, nil
		}
		return session.ToolResult{}, errors.New(name + " is not open to a tool's script")
	}}
}

// A script runs tools through the host and hands back only what it printed: what a call returned
// stays in the script.
func TestRunScriptCallsToolsAndHandsBackOnlyWhatItPrinted(t *testing.T) {
	var calls []recordedCall
	got := runScript(t, scriptTool(t), recordingEnv(&calls), map[string]any{"src": `
      local text = call_tool("read", { path = "a.md" })
      print("read", #text)
      local none, why = call_tool("grep", { pattern = "x" })
      print("grep", tostring(none), why)
      local refused, msg = call_tool("write", {})
      print("write", tostring(refused), msg)
      print(json_encode({ n = 2 }), json_decode('{"k":"v"}').k)`})
	want := "read\t15\ngrep\tnil\tno such pattern\nwrite\tnil\twrite is not open to a tool's script\n{\"n\":2}\tv\n|ERR:nil"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "hello from read") {
		t.Fatal("what a call returned leaked into the output")
	}
	if len(calls) != 3 || calls[0].name != "read" || calls[0].args != `{"path":"a.md"}` || calls[2].args != `{}` {
		t.Fatalf("calls reached the host as %+v", calls)
	}
}

// The script's state is not the plugin's: no files, processes, modules or loaders.
func TestRunScriptHasNoWayOutOfItsSandbox(t *testing.T) {
	var calls []recordedCall
	got := runScript(t, scriptTool(t), recordingEnv(&calls), map[string]any{"src": `
      print(type(os), type(io), type(require), type(dofile), type(loadfile), type(load), type(loadstring), type(magi))`})
	if got != "nil\tnil\tnil\tnil\tnil\tnil\tnil\tnil\n|ERR:nil" {
		t.Fatalf("the sandbox has a way out: %q", got)
	}
}

// A runaway script ends at its time limit, and output past its limit is cut and says so.
func TestRunScriptStopsAtItsLimits(t *testing.T) {
	var calls []recordedCall
	tool := scriptTool(t)
	start := time.Now()
	got := runScript(t, tool, recordingEnv(&calls), map[string]any{"src": `print("before") while true do end`, "timeout": 1})
	if time.Since(start) > 10*time.Second {
		t.Fatal("the time limit did not stop the script")
	}
	if !strings.HasPrefix(got, "before\n|ERR:") || !strings.Contains(got, "time limit") {
		t.Fatalf("a stopped script keeps what it printed and says why: %q", got)
	}
	got = runScript(t, tool, recordingEnv(&calls), map[string]any{"src": `for i = 1, 1000 do print("line " .. i) end`, "output": 100})
	body := strings.SplitN(got, "|ERR:", 2)[0]
	if !strings.Contains(body, "output cut") || len(body) > 200 || !strings.HasSuffix(got, "|ERR:nil") {
		t.Fatalf("output past its limit must be cut and say so: %d bytes %q", len(body), got)
	}
}

// Outside a tool call there is no host to run tools through.
func TestRunScriptNeedsAToolCall(t *testing.T) {
	got := runScript(t, scriptTool(t), port.ToolEnv{}, map[string]any{"src": `print("x")`})
	if got != "nil|ERR:run_script: only available inside a tool call" {
		t.Fatalf("got %q", got)
	}
}

func TestRunScriptGraphAdvancesAnIndependentBranchBeforeJoining(t *testing.T) {
	advanced := make(chan struct{})
	env := port.ToolEnv{
		ParallelToolSafe: func(string) bool { return true },
		CallTool: func(ctx context.Context, name string, _ json.RawMessage) (session.ToolResult, error) {
			switch name {
			case "grep":
				select {
				case <-advanced:
				case <-ctx.Done():
					return session.ToolResult{}, ctx.Err()
				}
			case "glob":
				close(advanced)
			}
			return session.ToolResult{Content: json.RawMessage(`"` + name + `"`)}, nil
		},
	}
	got := runScript(t, scriptTool(t), env, map[string]any{"timeout": 3, "src": `
local results = call_graph({
  {id="fast", tool="read"},
  {id="slow", tool="grep"},
  {id="next", tool="glob", after={"fast"}},
  {id="join", tool="list", after={"slow", "next"}},
})
print(results.join)`})
	if got != "list\n|ERR:nil" {
		t.Fatalf("the fast branch must advance while the slow branch waits: %q", got)
	}
}

func TestRunScriptGraphSkipsAJoinAfterAFailedBranch(t *testing.T) {
	var calls []recordedCall
	got := runScript(t, scriptTool(t), recordingEnv(&calls), map[string]any{"src": `
call_graph({
  {id="bad", tool="grep"},
  {id="good", tool="read"},
  {id="join", tool="list", after={"bad", "good"}},
})
print("joined")`})
	if !strings.Contains(got, "bad: no such pattern") || !strings.Contains(got, "join skipped: blocked by bad") || strings.Contains(got, "joined") || len(calls) != 2 {
		t.Fatalf("failure must block the join, with the independent branch still running: got %q calls %+v", got, calls)
	}
}
