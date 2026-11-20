package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// grapher reports the stages of where it is and keeps what the host said it is doing meanwhile.
type grapher struct {
	a    *App
	saw  []byte
	none bool
}

func (*grapher) Name() string            { return "grapher" }
func (*grapher) Description() string     { return "draws its run" }
func (*grapher) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (g *grapher) Execute(_ context.Context, _ json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.EmitStages == nil {
		g.none = true
		return session.ToolResult{Content: json.RawMessage(`"no EmitStages"`)}, nil
	}
	env.EmitStages("step 1", []event.ProgressStage{{ID: "s0", Label: "listing", Kind: "tool", State: "running", Done: 1, Total: 3, Running: 2}})
	st, _ := g.a.DoingStages(env.SessionID)
	g.saw, _ = json.Marshal(st)
	return session.ToolResult{Content: json.RawMessage(`"done"`)}, nil
}

// A running call's stages are kept where a screen in another process can ask for it (status, the
// roster), as its note is; it goes when the call returns.
func TestARunningCallsStagesAreKeptUntilItReturns(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := &grapher{a: a}
	a.tools.Register(g)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		&session.ToolCall{CallID: "g1", Name: "grapher", Args: json.RawMessage(`{}`)}, newRunGuard(nil), "")
	if g.none {
		t.Fatal("the host offers no EmitStages")
	}
	if string(g.saw) != `[{"id":"s0","label":"listing","kind":"tool","state":"running","done":1,"total":3,"running":2}]` {
		t.Fatalf("while running, DoingStages said %s", g.saw)
	}
	if left, ok := a.DoingStages(sid); ok || len(left) != 0 {
		t.Fatalf("the stages outlived their call: %+v", left)
	}
}
