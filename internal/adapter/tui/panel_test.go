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
