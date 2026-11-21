package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// scriptCaller stands in for a plugin whose script runs other tools through ToolEnv.CallTool:
// it runs the calls it is given and keeps what came back.
type scriptCaller struct {
	got  []string
	errs []string
}

func (*scriptCaller) Name() string            { return "script_caller" }
func (*scriptCaller) Description() string     { return "runs tools from a script" }
func (*scriptCaller) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *scriptCaller) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.CallTool == nil {
		return session.ToolResult{Content: json.RawMessage(`"no CallTool"`), IsError: true}, nil
	}
	var in struct {
		Calls []struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"calls"`
	}
	_ = json.Unmarshal(raw, &in)
	for _, call := range in.Calls {
		r, err := env.CallTool(ctx, call.Name, call.Args)
		if err != nil {
			c.errs = append(c.errs, err.Error())
			continue
		}
		if r.IsError {
			c.errs = append(c.errs, string(r.Content))
			continue
		}
		c.got = append(c.got, string(r.Content))
	}
	return session.ToolResult{Content: json.RawMessage(`"script finished"`)}, nil
}

func scriptArgs(t *testing.T, calls ...map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"calls": calls})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A script's calls go through the path a model's calls take and are recorded under their own
// names, so permission, recovery and the turn's steps see them; but what they returned stays in
// the script — the model's history does not carry it, or nothing was saved by scripting it.
func TestAToolsScriptRunsReadsThatTheModelHistoryDoesNotCarry(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	caller := &scriptCaller{}
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	note := filepath.Join(s.Workdir, "note.txt")
	if err := os.WriteFile(note, []byte("inner-secret-body-42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	made := filepath.Join(s.Workdir, "made.txt")
	args := scriptArgs(t,
		map[string]any{"name": "read", "args": map[string]any{"path": note}},
		map[string]any{"name": "write", "args": map[string]any{"path": made, "content": "x"}},
	)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	outer := &session.ToolCall{CallID: "outer", Name: "script_caller", Args: args}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, outer, newRunGuard(nil), "")

	if len(caller.got) != 2 || !strings.Contains(caller.got[0], "inner-secret-body-42") {
		t.Fatalf("the script did not get what read returned: got=%q errs=%q", caller.got, caller.errs)
	}
	// A script runs for the conversation it was called from, so its write follows that conversation's
	// mode: in allow mode it runs as the model's own write would.
	if len(caller.errs) != 0 {
		t.Fatalf("in allow mode a script's write must run like the model's own: %q", caller.errs)
	}
	if _, err := os.Stat(made); err != nil {
		t.Fatalf("an allowed write did not write: %v", err)
	}

	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	inner := ""
	started := map[string]bool{}
	for _, e := range evs {
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.ToolCall != nil && d.Part.ToolCall.Name == "read" {
				if d.Part.ToolCall.Parent != "outer" {
					t.Fatalf("the inner read must name its parent call: %+v", d.Part.ToolCall)
				}
				inner = d.Part.ToolCall.CallID
			}
		case event.TypeToolStarted:
			var d event.ToolStartedData
			if json.Unmarshal(e.Data, &d) == nil {
				started[d.CallID] = true
			}
		}
	}
	if inner == "" {
		t.Fatal("the inner read was not recorded under its own name")
	}
	if !started[inner] {
		t.Fatal("the inner read has no start fact: after a crash nobody could tell whether it ran")
	}
	if r := toolResultFor(t, a, sid, inner); r == nil {
		t.Fatal("the inner read has no recorded result")
	}

	msgs := reconstruct(evs)
	whole, _ := json.Marshal(msgs)
	if strings.Contains(string(whole), inner) {
		t.Fatalf("the model history carries the inner call %s", inner)
	}
	if strings.Contains(string(whole), "inner-secret-body-42") {
		t.Fatal("the model history carries what the script read")
	}
	if !strings.Contains(string(whole), "script finished") {
		t.Fatal("the outer call's own result must stay in the history")
	}
}

// One call cannot run calls without end: past the limit it is told so, in words.
func TestAToolsScriptStopsAtItsCallLimit(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	caller := &scriptCaller{}
	a.tools.Register(caller)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	calls := make([]map[string]any, 201)
	for i := range calls {
		calls[i] = map[string]any{"name": "list", "args": map[string]any{"path": s.Workdir}}
	}
	outer := &session.ToolCall{CallID: "outer", Name: "script_caller", Args: scriptArgs(t, calls...)}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	if len(caller.got) != 200 || len(caller.errs) != 1 || !strings.Contains(caller.errs[0], "limit") {
		t.Fatalf("want 200 calls and one refusal naming the limit: got %d, errs %q", len(caller.got), caller.errs)
	}
}
