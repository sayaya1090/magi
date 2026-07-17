package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A zoom left pointing at nothing (the followed live pane finished and was removed,
// or focus was cleared) must self-heal on refresh: exit the zoom and show the normal
// transcript. Before the guard, renderZoom returned "" while View() fell back to the
// overview header — a blank screen with no breadcrumb to click.
func TestRefreshSelfHealsEmptyZoom(t *testing.T) {
	m := newTestModel(t)
	m.ready = true
	m.width, m.height = 100, 30
	m.zoom = true
	m.zoomPane = nil
	m.focusPane = -1 // nothing to view

	m.refresh()

	if m.zoom {
		t.Fatal("refresh must exit a zoom that has no pane to show")
	}
}

// A plain click inside the agent detail (zoom) view that doesn't hit a reasoning
// block must be consumed there — never fall through to the overview's focus logic,
// which would clear focusPane and blank a live-follow zoom.
func TestZoomClickInsideKeepsDetail(t *testing.T) {
	m := newTestModel(t)
	m.ready = true
	m.width, m.height = 100, 30
	m.panes = append(m.panes, &agentPane{
		sid:  session.SessionID("child"),
		role: "coder",
		task: "do something",
		blocks: []block{
			{kind: blockAssistant, text: "working on it"},
		},
	})
	m.focusPane = 0
	m.zoom = true
	m.refresh()

	// Click + release mid-screen (not a reasoning block, not the breadcrumb).
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 5})
	m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 10, Y: 5})

	if !m.zoom {
		t.Fatal("a click inside the detail view must not exit the zoom")
	}
	if m.focusPane != 0 {
		t.Fatalf("a click inside the detail view must keep the followed pane focused, got %d", m.focusPane)
	}
	if m.viewedPane() == nil {
		t.Fatal("the detail view must still have a pane to show")
	}
}

// Clicking the breadcrumb header row while zoomed still goes back to the overview
// (regression guard around the click-consume fix: only in-body clicks are consumed).
func TestZoomBreadcrumbClickGoesBack(t *testing.T) {
	m := newTestModel(t)
	m.ready = true
	m.width, m.height = 100, 30
	m.panes = append(m.panes, &agentPane{
		sid:    session.SessionID("child"),
		role:   "coder",
		blocks: []block{{kind: blockAssistant, text: "done"}},
	})
	m.focusPane = 0
	m.zoom = true
	m.refresh()

	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 0})

	if m.zoom {
		t.Fatal("clicking the breadcrumb row must exit the zoom")
	}
}
