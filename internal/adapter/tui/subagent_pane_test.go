package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
)

// A spawned child opens a pane, the way a background command does.
//
// The strip, the detail view and the side panel already existed and had exactly one producer. A
// child runs for minutes on a session id this model does not subscribe to, so before this the only
// sign of it was the parent's spinner and one clipped line.
func TestASpawnedChildOpensAPaneOnTheStrip(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 120, 40

	started := time.Now().Add(-30 * time.Second)
	running := []app.SubagentJob{{
		ID: "child-1", Tool: "seele_plan", Task: "plan the refactor",
		Started: started, Running: true, Tail: "spawn · step 1 · read\n",
	}}
	if !m.syncSubagentPanes(running) {
		t.Fatal("a running child did not change the strip")
	}
	if len(m.panes) != 1 {
		t.Fatalf("the child opened %d panes", len(m.panes))
	}
	p := m.panes[0]
	if p.role != "seele_plan" {
		t.Errorf("the pane is labelled %q — a reader cannot tell which subagent this is", p.role)
	}
	if !strings.Contains(p.task, "plan the refactor") {
		t.Errorf("the pane does not say what the child was asked: %q", p.task)
	}
	if !strings.Contains(p.live, "step 1 · read") {
		t.Errorf("the child's progress is not in its pane: %q", p.live)
	}
	if p.done {
		t.Error("a running child's pane is already marked done")
	}

	// Polling again while it still runs must not open a second pane, and must not restart the fade.
	m.syncSubagentPanes(running)
	if len(m.panes) != 1 {
		t.Fatalf("polling opened %d panes for one child", len(m.panes))
	}

	// When it ends the pane ends with it — once.
	ended := []app.SubagentJob{{
		ID: "child-1", Tool: "seele_plan", Task: "plan the refactor",
		Started: started, Ended: time.Now(), Running: false, Steps: 7,
		Tail: "spawn · step 1 · read\n",
	}}
	m.syncSubagentPanes(ended)
	if !m.panes[0].done {
		t.Error("the child finished and its pane is still spinning")
	}
	if m.panes[0].dur <= 0 {
		t.Error("the pane does not say how long the child took")
	}
	first := m.panes[0].doneAt
	m.syncSubagentPanes(ended)
	if !m.panes[0].doneAt.Equal(first) {
		t.Error("polling restarted the fade — the pane would never leave the strip")
	}
}
