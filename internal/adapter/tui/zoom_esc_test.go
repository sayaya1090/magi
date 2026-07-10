package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// In the agent detail (zoom) screen, esc goes back — even when the zoomed
// subagent is still running. It must not fall through to the "interrupt the
// focused running pane" branch, which would keep the detail screen open.
func TestEscExitsZoomForRunningPane(t *testing.T) {
	m := newTestModel(t)
	m.panes = []*agentPane{{role: "coder", sid: "s1", done: false}} // still running
	m.focusPane = 0
	m.zoom = true

	_, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled {
		t.Fatal("esc should be handled in the zoom view")
	}
	if m.zoom {
		t.Fatal("esc in the detail screen should exit zoom (go back)")
	}
	if m.focusPane != 0 {
		t.Fatalf("exiting zoom should keep focus on the pane, got %d", m.focusPane)
	}
}

// A pinned finished-pane detail (zoomPane) also exits on esc.
func TestEscExitsZoomForFinishedPane(t *testing.T) {
	m := newTestModel(t)
	fin := &agentPane{role: "tester", sid: "s2", done: true}
	m.panes = []*agentPane{fin}
	m.zoomPane = fin
	m.zoom = true

	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !handled {
		t.Fatal("esc should be handled")
	}
	if m.zoom || m.zoomPane != nil {
		t.Fatalf("esc should exit zoom and drop the pin, got zoom=%v zoomPane=%v", m.zoom, m.zoomPane)
	}
}
