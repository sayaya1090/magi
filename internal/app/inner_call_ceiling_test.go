package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// labelTool has no outside effect; it is on no list written for any plugin.
type labelTool struct{}

func (labelTool) Name() string            { return "label" }
func (labelTool) Description() string     { return "labels the conversation" }
func (labelTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (labelTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{Content: json.RawMessage(`"labelled"`)}, nil
}

// The host decides what a script may call by what a tool IS (it only looks, it has no outside
// effect, its children only look), never by a list of names some plugin needs.
func TestAScriptCallsAToolThatOnlyLooksWithoutANameList(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	caller := &scriptCaller{}
	a.tools.Register(caller)
	a.tools.Register(labelTool{})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	outer := &session.ToolCall{CallID: "outer", Name: "script_caller", Args: scriptArgs(t, map[string]any{"name": "label", "args": map[string]any{}})}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	if len(caller.got) != 1 || !strings.Contains(caller.got[0], "labelled") {
		t.Fatalf("a tool with no outside effect was not run for the script: got=%q errs=%q", caller.got, caller.errs)
	}
}
