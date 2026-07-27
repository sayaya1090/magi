package tui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A subagent that delegates further must get a pane for ITS child. openPane was reachable only from
// the primary session's subscription, so a nested delegation left nothing on screen: the delegating
// pane fell silent for as long as the grandchild worked — measured at 828 seconds against 26 tool
// calls happening one level down — and there was nothing to open to see why.
func TestSpawnInsideAPaneOpensItsOwnPane(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := app.New(store, nil, builtin.Default(), bus.New(), nil, app.Config{Permission: "allow"})
	ctx := context.Background()
	child, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	m := newPaneModel()
	m.app, m.ctx, m.sid, m.mainSub = a, ctx, session.SessionID("s_main"), 1
	// The delegating subagent already has a pane; its events arrive on that subscription.
	m.subID = 7
	m.panes = []*agentPane{{sid: child, role: "default", sub: 7}}

	d, _ := json.Marshal(event.AgentStatusData{AgentID: string(grand), Parent: string(child), Role: "worker", State: "running"})
	spawn := eventMsg{sid: child, sub: 7, ev: event.Event{Type: event.TypeAgentSpawned, SessionID: child, Data: d}}
	// Update has a value receiver — bubbletea keeps the model it RETURNS, so that is the one to read.
	out, _ := m.Update(spawn)
	next := out.(Model)
	m = &next

	if p := m.paneBySID(grand); p == nil {
		t.Fatal("a spawn announced inside a pane must open a pane for that grandchild")
	} else if p.role != "worker" {
		t.Errorf("the grandchild pane carries its own role, got %q", p.role)
	}
	if len(m.panes) != 2 {
		t.Errorf("panes = %d; want 2 (the delegating child and its worker)", len(m.panes))
	}
	// Idempotent: the same announcement re-delivered must not stack a second pane.
	again, _ := m.Update(spawn)
	m2 := again.(Model)
	if len(m2.panes) != 2 {
		t.Errorf("a repeated spawn event must not open a duplicate pane, got %d", len(m2.panes))
	}
}
