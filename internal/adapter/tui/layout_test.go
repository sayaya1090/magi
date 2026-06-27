package tui

import (
	"fmt"
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
		if !strings.Contains(pv, "↓") {
			t.Errorf("running=%v: 30 agents on a 24-row screen should fold into a '+N more' line:\n%s", running, pv)
		}
	}
}

// paneScroll windows which panes are on screen and is clamped to the valid range
// so it can never scroll past the last pane or above the first.
func TestPaneScrollWindowsAndClamps(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", task: fmt.Sprintf("agent-%d", i)})
	}
	nShown, _, _, _ := m.paneLayout()
	maxOff := len(m.panes) - nShown

	// Scrolling down past the end clamps to maxOff; the last pane becomes visible.
	m.paneScroll = 999
	pv := m.renderPanes(m.width, 5)
	if m.paneScroll != maxOff {
		t.Fatalf("over-scroll should clamp to %d, got %d", maxOff, m.paneScroll)
	}
	if !strings.Contains(pv, fmt.Sprintf("agent-%d", len(m.panes)-1)) {
		t.Errorf("max scroll should reveal the last pane:\n%s", pv)
	}
	// The no-drift invariant holds at a non-zero offset too: render height == reserve.
	if h := lipgloss.Height(pv); h != m.panesBlockHeight() {
		t.Fatalf("scrolled render height %d != reserved %d (drift at offset)", h, m.panesBlockHeight())
	}
	// Scrolling up past the top clamps to 0; the first pane is visible again.
	m.paneScroll = -5
	pv = m.renderPanes(m.width, 5)
	if m.paneScroll != 0 {
		t.Fatalf("under-scroll should clamp to 0, got %d", m.paneScroll)
	}
	if !strings.Contains(pv, "agent-0") {
		t.Errorf("zero scroll should show the first pane:\n%s", pv)
	}
}

// In a focused pane list, ↓ moves the selection and the window follows it so the
// focused pane can never scroll out of view.
func TestPaneKeyFocusFollowsWindow(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", task: fmt.Sprintf("agent-%d", i)})
	}
	m.focusPane = 0
	nShown, _, _, _ := m.paneLayout()
	// Press ↓ past the bottom of the first window; focus and window advance together.
	for i := 0; i < nShown+2; i++ {
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.focusPane != nShown+2 {
		t.Fatalf("↓ should advance focus to %d, got %d", nShown+2, m.focusPane)
	}
	if m.focusPane < m.paneScroll || m.focusPane >= m.paneScroll+nShown {
		t.Fatalf("focused pane %d scrolled out of window [%d,%d)", m.focusPane, m.paneScroll, m.paneScroll+nShown)
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
