package tui

import "testing"

// Dragging the panel's left edge resizes it, clamped to a sensible range.
func TestPanelSplitterResize(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.panelW = 44
	m.panes = []*agentPane{{role: "coder"}} // makes hasPanel true

	// Splitter sits at width - panelCols = 100 - 45 = 55.
	if !m.onPanelSplitter(55) {
		t.Fatal("column 55 should be on the splitter")
	}
	if m.onPanelSplitter(10) {
		t.Fatal("column 10 should not be on the splitter")
	}

	// Drag right → narrower panel: w = width - x - 1 = 100 - 70 - 1 = 29.
	m.setPanelWidthForSplit(70)
	if m.panelW != 29 {
		t.Fatalf("panelW = %d, want 29", m.panelW)
	}
	// Clamp to minimum (24).
	m.setPanelWidthForSplit(95)
	if m.panelW != 24 {
		t.Fatalf("min clamp = %d, want 24", m.panelW)
	}
	// Clamp to maximum (width/2 - 1 = 49).
	m.setPanelWidthForSplit(2)
	if m.panelW != 49 {
		t.Fatalf("max clamp = %d, want 49", m.panelW)
	}
}

// Clicking a subagent row in the right panel opens that subagent's detail view
// (focus + zoom), like clicking its pane. Drives the real render path so the
// hit-test Y is recorded on the shared *agentPane (View has a value receiver, so
// it must persist via the pointer, not a Model value field).
func TestHandlePanelClick(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height, m.ready = 100, 40, true
	m.vp.SetWidth(60)
	m.vp.SetHeight(20)
	m.panes = []*agentPane{{role: "coder"}, {role: "tester"}}

	_ = m.View() // records each subagent's absolute panelY on the *agentPane
	if m.panes[0].panelY <= 0 || m.panes[1].panelY <= 0 {
		t.Fatalf("subagent rows not recorded via View: %d, %d", m.panes[0].panelY, m.panes[1].panelY)
	}
	if m.panes[1].panelY == m.panes[0].panelY {
		t.Fatal("the two subagent rows should be on different lines")
	}

	inPanelX := m.width - 2 // a column inside the right panel

	// Click the 2nd subagent → zoom into it.
	if !m.handlePanelClick(inPanelX, m.panes[1].panelY) {
		t.Fatal("click on a subagent row should be consumed")
	}
	if m.focusPane != 1 || !m.zoom {
		t.Fatalf("expected focus=1 zoom=true, got focus=%d zoom=%v", m.focusPane, m.zoom)
	}

	// A click left of the panel is ignored.
	m.focusPane, m.zoom = -1, false
	if m.handlePanelClick(2, m.panes[0].panelY) {
		t.Fatal("click outside the panel should not be consumed")
	}
	// A click on a non-subagent row (the header just above the first entry) is ignored.
	if m.handlePanelClick(inPanelX, m.panes[0].panelY-1) {
		t.Fatal("click on a non-subagent panel row should not be consumed")
	}
}
