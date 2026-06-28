package tui

import "testing"

// The floating post-it has a fixed width — there is no draggable splitter.
func TestNoPanelSplitter(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.panes = []*agentPane{{role: "coder"}} // makes hasPanel true
	if m.onPanelSplitter(55) || m.onPanelSplitter(10) {
		t.Fatal("the floating panel has no splitter; onPanelSplitter must always be false")
	}
}

// Clicking a subagent row in the floating post-it opens that subagent's detail view
// (focus + zoom), like clicking its pane. Drives the real render path so the hit-test
// Y is recorded on the shared *agentPane (View has a value receiver, so it must
// persist via the pointer, not a Model value field).
func TestHandlePanelClick(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height, m.ready = 100, 40, true
	m.vp.SetWidth(100)
	m.vp.SetHeight(20)
	m.panes = []*agentPane{{role: "coder"}, {role: "tester"}}

	_ = m.View() // records each subagent's absolute panelY on the *agentPane
	box, top, left, ok := m.floatPanel()
	if !ok {
		t.Fatal("the post-it should be shown when there are subagents")
	}
	_ = box
	if m.panes[0].panelY <= 0 || m.panes[1].panelY <= 0 {
		t.Fatalf("subagent rows not recorded via View: %d, %d", m.panes[0].panelY, m.panes[1].panelY)
	}
	if m.panes[1].panelY == m.panes[0].panelY {
		t.Fatal("the two subagent rows should be on different lines")
	}
	inBoxX := left + 2 // a column inside the post-it box

	// Click the 2nd subagent → zoom into it.
	if !m.handlePanelClick(inBoxX, m.panes[1].panelY) {
		t.Fatal("click on a subagent row should be consumed")
	}
	if m.focusPane != 1 || !m.zoom {
		t.Fatalf("expected focus=1 zoom=true, got focus=%d zoom=%v", m.focusPane, m.zoom)
	}

	// A click outside the box (far left, in the transcript) is ignored.
	m.focusPane, m.zoom = -1, false
	if m.handlePanelClick(2, m.panes[0].panelY) {
		t.Fatal("click outside the post-it should not be consumed")
	}
	// A click inside the box but not on a subagent row (the top border row) is CONSUMED —
	// so it doesn't fall through to the transcript — but changes no focus.
	m.focusPane, m.zoom = -1, false
	if !m.handlePanelClick(inBoxX, top) {
		t.Fatal("click on empty post-it area should be consumed (not fall through)")
	}
	if m.focusPane != -1 || m.zoom {
		t.Fatalf("empty-area click should not change focus/zoom, got focus=%d zoom=%v", m.focusPane, m.zoom)
	}
}
