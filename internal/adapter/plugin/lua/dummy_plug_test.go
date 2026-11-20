package lua

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The shipped dummy-plug plugin, loaded from the repo: a model enters the mode, runs a script that
// calls tools through the host, and leaves; outside the mode a script is refused, and a finished
// turn leaves the mode on its own.
func TestDummyPlugEntersRunsAndLeaves(t *testing.T) {
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
	tool := func(name string) port.Tool {
		tl, ok := sink.Get(name)
		if !ok {
			t.Fatalf("dummy-plug registered no %s", name)
		}
		return tl
	}
	enter, run, exit := tool("dummy_plug_enter"), tool("dummy_plug_run"), tool("dummy_plug_exit")
	var called []string
	env := func(sid string) port.ToolEnv {
		return port.ToolEnv{SessionID: session.SessionID(sid), CallTool: func(_ context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
			called = append(called, name+" "+string(args))
			return session.ToolResult{Content: json.RawMessage(`"one\ntwo\nthree"`)}, nil
		}}
	}
	call := func(tl port.Tool, sid string, args string) (string, bool) {
		res, err := tl.Execute(context.Background(), json.RawMessage(args), env(sid))
		if err != nil {
			t.Fatal(err)
		}
		var s string
		_ = json.Unmarshal(res.Content, &s)
		return s, res.IsError
	}
	script := `{"script":"local t = call_tool('read', {path='a.md'}) local n = 0 for _ in t:gmatch('[^\\n]+') do n = n + 1 end print('lines', n)"}`

	if got, isErr := call(run, "s1", script); !isErr || !strings.Contains(got, "dummy_plug_enter") {
		t.Fatalf("outside the mode a script is refused and told how to enter: %q %v", got, isErr)
	}
	if len(called) != 0 {
		t.Fatal("a refused script still ran tools")
	}
	got, isErr := call(enter, "s1", `{}`)
	if isErr || !strings.Contains(got, "call_tool") || !strings.Contains(got, "read") || !strings.Contains(got, "print") {
		t.Fatalf("entering says what a script can use: %q", got)
	}
	if got, isErr := call(run, "s1", script); isErr || got != "lines\t3\n" {
		t.Fatalf("in the mode a script runs and only its print comes back: %q %v", got, isErr)
	}
	if len(called) != 1 || called[0] != `read {"path":"a.md"}` {
		t.Fatalf("the script's call reached the host as %q", called)
	}
	if _, isErr := call(run, "s2", script); !isErr {
		t.Fatal("the mode belongs to the session that entered it")
	}
	if got, isErr := call(run, "s1", `{"script":"error('boom')"}`); !isErr || !strings.Contains(got, "boom") {
		t.Fatalf("a failing script is an error that says why: %q", got)
	}
	if _, isErr := call(exit, "s1", `{}`); isErr {
		t.Fatal("leaving failed")
	}
	if _, isErr := call(run, "s1", script); !isErr {
		t.Fatal("after leaving, a script is refused again")
	}

	call(enter, "s1", `{}`)
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "done"})
	h.DrainEvents(5 * time.Second) // events are delivered on the host's own goroutine
	if _, isErr := call(run, "s1", script); !isErr {
		t.Fatal("a finished turn leaves the mode")
	}
}
