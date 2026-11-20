package lua

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The shipped dummy-plug plugin is one tool. A script runs at once — there is no mode to enter or
// leave — and, where the host offers a child session, it runs inside one: its calls go to the
// child's CallTool, never the parent's, the child is named by the goal, and it is closed with what
// the script printed (or why it failed). Without a child the script runs on the call itself.
func TestDummyPlugIsOneToolThatRunsItsScriptInAChild(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "dummy-plug"))
	if err != nil {
		t.Fatal(err)
	}
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, ContextReg: &fakeContextReg{},
		Notify: func(string, string) {}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load dummy-plug: %v", err)
	}
	for _, gone := range []string{"dummy_plug_enter", "dummy_plug_exit"} {
		if _, ok := sink.Get(gone); ok {
			t.Fatalf("%s is still registered: a script runs at once", gone)
		}
	}
	run, ok := sink.Get("dummy_plug_run")
	if !ok {
		t.Fatal("dummy-plug registered no dummy_plug_run")
	}
	if d := run.Description(); !strings.Contains(d, "call_tool") || !strings.Contains(d, "plan(") || !strings.Contains(d, "print") {
		t.Fatalf("the tool's description must say what a script can use: %q", d)
	}
	var parentCalls, childCalls []string
	var opened []string
	var closed []string
	parent := func(_ context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		parentCalls = append(parentCalls, name+" "+string(args))
		return session.ToolResult{Content: json.RawMessage(`"one\ntwo\nthree"`)}, nil
	}
	withChild := port.ToolEnv{SessionID: "s1", CallTool: parent, OpenChild: func(_ context.Context, label string) (port.ChildRun, error) {
		opened = append(opened, label)
		return port.ChildRun{SessionID: "child1",
			CallTool: func(_ context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
				childCalls = append(childCalls, name+" "+string(args))
				return session.ToolResult{Content: json.RawMessage(`"one\ntwo\nthree"`)}, nil
			},
			Close: func(text string, failed error) {
				why := ""
				if failed != nil {
					why = failed.Error()
				}
				closed = append(closed, text+"|"+why)
			}}, nil
	}}
	call := func(env port.ToolEnv, args string) (string, bool) {
		res, err := run.Execute(context.Background(), json.RawMessage(args), env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		_ = json.Unmarshal(res.Content, &s)
		return s, res.IsError
	}
	script := `"script":"local t = call_tool('read', {path='a.md'}) local n = 0 for _ in t:gmatch('[^\\n]+') do n = n + 1 end print('lines', n)"`

	if got, isErr := call(withChild, `{"goal":"count the lines of a.md",`+script+`}`); isErr || got != "lines\t3\n" {
		t.Fatalf("a script runs at once and only its print comes back: %q %v", got, isErr)
	}
	if len(opened) != 1 || !strings.Contains(opened[0], "count the lines of a.md") {
		t.Fatalf("the child must be opened once, named by the goal: %q", opened)
	}
	if len(childCalls) != 1 || childCalls[0] != `read {"path":"a.md"}` || len(parentCalls) != 0 {
		t.Fatalf("the script's calls must run in the child only: child=%q parent=%q", childCalls, parentCalls)
	}
	if len(closed) != 1 || !strings.HasPrefix(closed[0], "lines\t3") || !strings.HasSuffix(closed[0], "|") {
		t.Fatalf("the child must be closed once with what the script printed: %q", closed)
	}

	if got, isErr := call(withChild, `{"script":"error('boom')"}`); !isErr || !strings.Contains(got, "boom") {
		t.Fatalf("a failing script is an error that says why: %q", got)
	}
	if len(closed) != 2 || !strings.Contains(closed[1], "boom") {
		t.Fatalf("a failed script closes its child with why: %q", closed)
	}

	bare := port.ToolEnv{SessionID: "s2", CallTool: parent}
	if got, isErr := call(bare, `{`+script+`}`); isErr || got != "lines\t3\n" || len(parentCalls) != 1 {
		t.Fatalf("without a child the script runs on the call itself: %q %v %q", got, isErr, parentCalls)
	}
	_ = errors.New
}
