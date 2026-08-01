package tui

import (
	"strings"
	"testing"
)

// Clicking a FINISHED subagent in the status panel (one that has faded into the
// done roster, so it's no longer in m.panes) must still open its detail — it pins
// the pane via zoomPane. This regressed when the panel became a floating post-it.
func TestPanelClickOpensFinishedSubagentDetail(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 120
	m.height = 40
	m.panelW = 40

	// A finished subagent that has faded out of the inline strip into the roster.
	done := &agentPane{sid: "child", role: "explore", sub: 1, done: true}
	m.doneRoster = []*agentPane{done}

	// Rendering the post-it assigns each row its on-screen Y (panelY).
	box, _, left, ok := m.floatPanel()
	if !ok {
		t.Fatalf("float panel should be visible (box=%q)", box)
	}
	if done.panelY <= 0 {
		t.Fatalf("a finished pane should still get a clickable panelY, got %d", done.panelY)
	}

	// Click that row → zoom opens onto the finished pane.
	if !m.handlePanelClick(left+2, done.panelY) {
		t.Fatal("a click on the finished subagent row should be consumed")
	}
	if !m.zoom || m.zoomPane != done {
		t.Errorf("click should pin+zoom the finished pane; zoom=%v zoomPane==done=%v", m.zoom, m.zoomPane == done)
	}
	// The zoom view is showing THAT pane. Its body is empty, and that is the honest state: a
	// pane's content is its live regions, a finished pane has none, and the per-pane transcript
	// that used to fill it lost its producer in 0c953b9. The click contract is what this test
	// owns — where the detail points — not what the detail has to say.
	if got := m.viewedPane(); got != done {
		t.Errorf("the detail is pointing at %v, not the pane that was clicked", got)
	}
	if out := m.renderZoom(80); strings.TrimSpace(ansiSeq.ReplaceAllString(out, "")) != "" {
		t.Logf("a finished pane's detail body is no longer empty — worth a look: %q", out)
	}

	// A sibling fade-out must NOT eject the pinned detail.
	m.advancePaneFade()
	if !m.zoom || m.zoomPane != done {
		t.Errorf("pinned finished-pane zoom should survive fade housekeeping; zoom=%v", m.zoom)
	}

	// exitZoom drops the pin.
	m.exitZoom()
	if m.zoom || m.zoomPane != nil {
		t.Errorf("exitZoom should clear zoom + pin; zoom=%v zoomPane!=nil=%v", m.zoom, m.zoomPane != nil)
	}
}

// Clicking an ACTIVE pane row focuses it by index and clears any finished-pane pin.
func TestPanelClickFocusesActivePane(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 120
	m.height = 40
	m.panelW = 40
	m.zoomPane = &agentPane{sid: "stale"} // a leftover pin to be cleared

	active := &agentPane{sid: "live", role: "coder", sub: 1}
	m.panes = []*agentPane{active}

	_, _, left, ok := m.floatPanel()
	if !ok {
		t.Fatal("float panel should be visible")
	}
	if active.panelY <= 0 {
		t.Fatalf("active pane should get a panelY, got %d", active.panelY)
	}
	if !m.handlePanelClick(left+2, active.panelY) {
		t.Fatal("a click on the active subagent row should be consumed")
	}
	if m.focusPane != 0 || m.zoomPane != nil || !m.zoom {
		t.Errorf("active click → focusPane=0, no pin, zoom on; got focus=%d pin=%v zoom=%v", m.focusPane, m.zoomPane != nil, m.zoom)
	}
}
