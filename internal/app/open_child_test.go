package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// childCaller opens a child session and runs its calls there, as a tool that drives a child itself
// (no model) does.
type childCaller struct {
	child string
	got   []string
	note  string
	// fail closes the child with this reason; leave closes it not at all (the process died mid-script).
	fail  string
	leave bool
}

func (*childCaller) Name() string            { return "child_caller" }
func (*childCaller) Description() string     { return "runs tools in a child it drives" }
func (*childCaller) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *childCaller) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.OpenChild == nil {
		c.note = "no OpenChild"
		return session.ToolResult{Content: json.RawMessage(`"no OpenChild"`), IsError: true}, nil
	}
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &in)
	ch, err := env.OpenChild(ctx, "read one note")
	if err != nil {
		c.note = err.Error()
		return session.ToolResult{Content: json.RawMessage(`"open failed"`), IsError: true}, nil
	}
	c.child = ch.SessionID
	args, _ := json.Marshal(map[string]string{"path": in.Path})
	r, err := ch.CallTool(ctx, "read", args)
	if err == nil && !r.IsError {
		c.got = append(c.got, string(r.Content))
	}
	switch {
	case c.leave:
	case c.fail != "":
		ch.Close("", errors.New(c.fail))
	default:
		ch.Close("read it", nil)
	}
	return session.ToolResult{Content: json.RawMessage(`"child finished"`)}, nil
}

// A tool may run its calls in a child session it drives itself, with no model in it: each call is
// recorded in the child's log (start fact, result) and the parent's log carries only the tool's own
// call and result. The child is a child of the parent and is not left running.
func TestAToolRunsItsCallsInAChildSessionItDrives(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	caller := &childCaller{}
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	note := filepath.Join(s.Workdir, "note.txt")
	if err := os.WriteFile(note, []byte("child-only-body-7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": note})
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	outer := &session.ToolCall{CallID: "outer", Name: "child_caller", Args: args}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, outer, newRunGuard(nil), "")

	if caller.note != "" {
		t.Fatalf("the host did not open a child: %s", caller.note)
	}
	if len(caller.got) != 1 || !strings.Contains(caller.got[0], "child-only-body-7") {
		t.Fatalf("the call in the child did not come back: %q", caller.got)
	}
	kids, err := a.store.ChildSessions(ctx, s.Workdir, string(sid))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range kids {
		if string(k.ID) == caller.child {
			found = true
		}
	}
	if caller.child == "" || caller.child == string(sid) || !found {
		t.Fatalf("the child %q is not a child session of %s (children: %+v)", caller.child, sid, kids)
	}

	parentLog, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range parentLog {
		if strings.Contains(string(e.Data), "child-only-body-7") {
			t.Fatalf("the parent's log carries what the child read: %s %s", e.Type, e.Data)
		}
		var d event.PartAppendedData
		if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.ToolCall != nil && d.Part.ToolCall.Name == "read" {
			t.Fatalf("the parent's log carries the child's read call: %+v", d.Part.ToolCall)
		}
	}
	childLog, err := a.store.Read(ctx, session.SessionID(caller.child), 0)
	if err != nil {
		t.Fatal(err)
	}
	read, started := "", map[string]bool{}
	for _, e := range childLog {
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.ToolCall != nil && d.Part.ToolCall.Name == "read" {
				read = d.Part.ToolCall.CallID
			}
		case event.TypeToolStarted:
			var d event.ToolStartedData
			if json.Unmarshal(e.Data, &d) == nil {
				started[d.CallID] = true
			}
		}
	}
	if read == "" || !started[read] {
		t.Fatalf("the child's log must carry the read and its start fact (read=%q)", read)
	}
	if r := toolResultFor(t, a, session.SessionID(caller.child), read); r == nil {
		t.Fatal("the child's read has no recorded result in the child's log")
	}
	if open, ok := unfinishedTurn(childLog); ok {
		t.Fatalf("a child that finished reads as cut off to recovery: %+v", open)
	}
	a.mu.Lock()
	_, left := a.states[session.SessionID(caller.child)]
	a.mu.Unlock()
	if left {
		t.Fatal("a closed child left its state behind")
	}
}

// runChild runs childCaller once and returns the child's log.
func runChild(t *testing.T, caller *childCaller) []event.Event {
	t.Helper()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	note := filepath.Join(s.Workdir, "note.txt")
	if err := os.WriteFile(note, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": note})
	outer := &session.ToolCall{CallID: "outer", Name: "child_caller", Args: args}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	if caller.child == "" {
		t.Fatalf("no child: %s", caller.note)
	}
	evs, err := a.store.Read(ctx, session.SessionID(caller.child), 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// A child closed with a failure is ended too, and says why in its own log.
func TestAFailedChildEndsWithItsReason(t *testing.T) {
	evs := runChild(t, &childCaller{fail: "script broke: boom"})
	if open, ok := unfinishedTurn(evs); ok {
		t.Fatalf("a child that failed reads as cut off to recovery: %+v", open)
	}
	said := false
	for _, e := range evs {
		if e.Type == event.TypeError && strings.Contains(string(e.Data), "script broke: boom") {
			said = true
		}
	}
	if !said {
		t.Fatal("the child's log does not say why it failed")
	}
}

// A child never closed — the process died while the script ran — stays an open turn, so recovery
// takes it up instead of reading it as done.
func TestAChildNeverClosedIsLeftForRecovery(t *testing.T) {
	evs := runChild(t, &childCaller{leave: true})
	if _, ok := unfinishedTurn(evs); !ok {
		t.Fatal("a child cut off mid-script must read as unfinished")
	}
}

func TestDummyPlugGraphFailureIsRecordedInTheChildSession(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	ctx := context.Background()
	parent := a.sessionInfo(ctx, sid)
	host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), Logf: func(message string) { t.Log(message) }})
	if _, err := host.Load(ctx, filepath.Join("..", "..", "plugins", "dummy-plug")); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"script": `call_graph({
   {id="missing", tool="read", args={path="workflow-missing-input.txt"}},
   {id="join", tool="list", after={"missing"}},
 })`})
	if err != nil {
		t.Fatal(err)
	}
	outer := &session.ToolCall{CallID: "graph", Name: "dummy_plug", Args: args}
	a.executeTool(ctx, parent, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	children, err := a.store.ChildSessions(ctx, parent.Workdir, string(sid))
	if err != nil || len(children) != 1 {
		t.Fatalf("want one persisted child, got %v (%v)", children, err)
	}
	log, err := a.store.Read(ctx, children[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range log {
		if e.Type == event.TypeError && strings.Contains(string(e.Data), "workflow-missing-input.txt") && strings.Contains(string(e.Data), "join skipped: blocked by missing") {
			if log[len(log)-1].Type != event.TypeTurnFinished {
				t.Fatal("child did not record its terminal event")
			}
			return
		}
	}
	t.Fatalf("child log lacks the failed input and blocked join: %+v", log)
}

func TestDummyPlugBackgroundReportsTheChildFailure(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, &fakeLLM{}, &scriptPlatform{}, Config{Permission: "allow"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), Logf: func(message string) { t.Log(message) }})
	if _, err := host.Load(ctx, filepath.Join("..", "..", "plugins", "dummy-plug")); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"background": true, "script": `call_graph({
   {id="missing", tool="read", args={path="background-missing-input.txt"}},
   {id="join", tool="list", after={"missing"}},
 })`})
	if err != nil {
		t.Fatal(err)
	}
	parent := a.sessionInfo(ctx, sid)
	outer := &session.ToolCall{CallID: "background-graph", Name: "dummy_plug", Args: args}
	a.executeTool(ctx, parent, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	cancel()
	result := toolResultFor(t, a, sid, outer.CallID)
	if result == nil || !result.Background || result.IsError {
		t.Fatalf("expected background launch result, got %+v", result)
	}
	waitFor(t, "background completion note", func() bool { _, notes, _ := countTurnsAndNotes(t, a, sid); return notes == 1 })
	_, _, note := countTurnsAndNotes(t, a, sid)
	if !strings.Contains(note, "background-missing-input.txt") || !strings.Contains(note, "join skipped: blocked by missing") {
		t.Fatalf("completion note lost the script failure: %s", note)
	}
	children, err := a.store.ChildSessions(context.Background(), parent.Workdir, string(sid))
	if err != nil || len(children) != 1 {
		t.Fatalf("want one child, got %v (%v)", children, err)
	}
	entries, err := a.store.Read(context.Background(), children[0].ID, 0)
	if err != nil || len(entries) == 0 || entries[len(entries)-1].Type != event.TypeTurnFinished {
		t.Fatalf("child has no terminal event: %v (%v)", entries, err)
	}
}
