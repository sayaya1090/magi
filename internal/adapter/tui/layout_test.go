package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The pane block must never push the input off-screen: chrome + a minimum
// viewport always fits the height, and the rendered pane height matches the
// reserved block (no reserve/render drift). Overflow folds into "+N more".
func TestPaneLayoutKeepsInputOnScreen(t *testing.T) {
	for _, running := range []bool{true, false} {
		mm := newTestModel(t)
		m := &mm
		m.width, m.height = 80, 24
		m.running = running
		for i := 0; i < 30; i++ {
			m.panes = append(m.panes, &agentPane{role: "explore"})
		}
		if got := m.chromeHeight() + minViewport; got > m.height {
			t.Fatalf("running=%v: chrome+minViewport = %d > height %d (input pushed off-screen)", running, got, m.height)
		}
		pv := m.renderPanes(m.width, 5)
		if h := lipgloss.Height(pv); h != m.panesBlockHeight() {
			t.Fatalf("running=%v: rendered panes %d != reserved panesBlockHeight %d (drift)", running, h, m.panesBlockHeight())
		}
		if !strings.Contains(pv, "more agent") {
			t.Errorf("running=%v: 30 agents on a 24-row screen should fold into a '+N more' line:\n%s", running, pv)
		}
	}
}

// Panes hidden behind "+N more" must have their hit-test rect cleared, so a click
// can't route to a pane that isn't on screen.
func TestPaneLayoutClearsHiddenRects(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", y: 99, h: 5}) // stale rects
	}
	m.renderPanes(m.width, 5)
	nShown, _, _, _ := m.paneLayout()
	for i := nShown; i < len(m.panes); i++ {
		if m.panes[i].h != 0 || m.panes[i].y != 0 {
			t.Fatalf("hidden pane %d kept a stale rect (y=%d h=%d) → click could misroute", i, m.panes[i].y, m.panes[i].h)
		}
	}
}

// With few agents and a tall screen, all panes show and there's no "+N more".
func TestPaneLayoutShowsAllWhenRoomy(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 60
	m.running = true
	m.panes = []*agentPane{{role: "a"}, {role: "b"}}
	nShown, _, more, _ := m.paneLayout()
	if nShown != 2 || more != 0 {
		t.Fatalf("roomy screen should show both panes with no overflow, got nShown=%d more=%d", nShown, more)
	}
	if m.chromeHeight()+minViewport > m.height {
		t.Fatalf("invariant broken even when roomy")
	}
}

// The input box must sit at the bottom: the rendered view fills the screen height
// even with a short transcript and several running panes (no floating gap).
func TestViewFillsHeightWithPanes(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = mm.(Model)
	m.running = true
	m.panes = []*agentPane{{role: "a"}, {role: "b"}, {role: "c"}}
	m.refresh()
	v := m.View()
	if h := lipgloss.Height(v.Content); h != 30 {
		t.Fatalf("view content height %d, want 30 — input box floats with empty space below when shorter", h)
	}
}
