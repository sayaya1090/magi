package tui

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func newPaneModel() *Model {
	applyTheme(true)
	return &Model{focusPane: -1, roleColor: map[string]int{}}
}

// Each role gets a stable, distinct palette index within a session.
func TestRoleColorStable(t *testing.T) {
	m := newPaneModel()
	a1 := m.roleColorIndex("explore")
	b1 := m.roleColorIndex("coder")
	a2 := m.roleColorIndex("explore")
	if a1 != a2 {
		t.Fatalf("explore color not stable: %d vs %d", a1, a2)
	}
	if a1 == b1 {
		t.Fatalf("explore and coder share a color index %d", a1)
	}
}

// Same-role panes share the hue but differ by brightness (color = identifier).
func TestSameRolePanesDifferByBrightness(t *testing.T) {
	m := newPaneModel()
	p1 := &agentPane{role: "coder", sid: "s1"}
	p2 := &agentPane{role: "coder", sid: "s2"}
	m.panes = []*agentPane{p1, p2}

	if m.paneInstanceIndex(p1) != 0 || m.paneInstanceIndex(p2) != 1 {
		t.Fatalf("instance indices = %d,%d want 0,1", m.paneInstanceIndex(p1), m.paneInstanceIndex(p2))
	}
	c1 := m.paneColorOf(p1)
	c2 := m.paneColorOf(p2)
	r1, g1, b1, _ := c1.RGBA()
	r2, g2, b2, _ := c2.RGBA()
	if r1 == r2 && g1 == g2 && b1 == b2 {
		t.Fatal("2nd same-role pane should have a brightness-shifted color")
	}
}

// taskAgents extracts both the single-agent and parallel-tasks forms.
func TestTaskAgents(t *testing.T) {
	if got := taskAgents(`{"agent":"explore","prompt":"x"}`); len(got) != 1 || got[0] != "explore" {
		t.Fatalf("single form: %v", got)
	}
	got := taskAgents(`{"tasks":[{"agent":"explore"},{"agent":"coder"}]}`)
	if len(got) != 2 || got[0] != "explore" || got[1] != "coder" {
		t.Fatalf("tasks form: %v", got)
	}
	if got := taskAgents(`not json`); got != nil {
		t.Fatalf("bad json should yield nil, got %v", got)
	}
}

// cyclePaneFocus walks main → panes → wrap.
func TestCyclePaneFocus(t *testing.T) {
	m := newPaneModel()
	m.panes = []*agentPane{{role: "a"}, {role: "b"}}
	if m.focusPane != -1 {
		t.Fatalf("start focus = %d, want -1 (main)", m.focusPane)
	}
	m.cyclePaneFocus(1)
	if m.focusPane != 0 {
		t.Fatalf("after 1: %d want 0", m.focusPane)
	}
	m.cyclePaneFocus(1)
	if m.focusPane != 1 {
		t.Fatalf("after 2: %d want 1", m.focusPane)
	}
	m.cyclePaneFocus(1)
	if m.focusPane != -1 {
		t.Fatalf("after 3 should wrap to main: %d want -1", m.focusPane)
	}
}

// applyPaneEvent folds part events and marks done on turn finish.
func TestApplyPaneEvent(t *testing.T) {
	m := newPaneModel()
	p := &agentPane{role: "explore"}

	delta, _ := json.Marshal(event.PartDeltaData{Kind: session.PartText, Text: "hi"})
	m.applyPaneEvent(p, event.Event{Type: event.TypePartDelta, Data: delta})
	if p.live != "hi" {
		t.Fatalf("live = %q want hi", p.live)
	}

	app, _ := json.Marshal(event.PartAppendedData{Part: session.Part{Kind: session.PartText, Text: "done text"}})
	m.applyPaneEvent(p, event.Event{Type: event.TypePartAppended, Data: app})
	if p.live != "" || len(p.blocks) != 1 {
		t.Fatalf("append: live=%q blocks=%d", p.live, len(p.blocks))
	}

	m.applyPaneEvent(p, event.Event{Type: event.TypeTurnFinished})
	if !p.done {
		t.Fatal("pane should be done after turn finished")
	}
}

// handlePaneClick focuses by row; a second click on the focused pane zooms.
func TestHandlePaneClick(t *testing.T) {
	m := newPaneModel()
	m.panes = []*agentPane{{role: "a"}, {role: "b"}}
	m.panes[0].y, m.panes[0].h = 10, 5 // rows 10-14
	m.panes[1].y, m.panes[1].h = 15, 5 // rows 15-19

	if !m.handlePaneClick(16) || m.focusPane != 1 {
		t.Fatalf("click row16 should focus pane 1, got %d", m.focusPane)
	}
	if !m.handlePaneClick(16) || !m.zoom {
		t.Fatal("second click on focused pane should zoom")
	}
	m.zoom = false
	if m.handlePaneClick(99) {
		t.Fatal("click outside any pane should not be consumed")
	}
}

// paneBySID / paneBySub locate panes.
func TestPaneLookup(t *testing.T) {
	m := newPaneModel()
	p := &agentPane{sid: session.SessionID("s_child"), sub: 7}
	m.panes = []*agentPane{p}
	if m.paneBySID("s_child") != p {
		t.Fatal("paneBySID failed")
	}
	if m.paneBySub(7) != p {
		t.Fatal("paneBySub failed")
	}
	if m.paneBySID("nope") != nil || m.paneBySub(99) != nil {
		t.Fatal("lookups should miss")
	}
}
