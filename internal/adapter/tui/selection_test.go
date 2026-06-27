package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// screenToContent maps a screen cell to a (content line, column).
func TestScreenToContent(t *testing.T) {
	m := newTestModel(t)
	m.contentPlain = make([]string, 100)
	for i := range m.contentPlain {
		m.contentPlain[i] = "hello world"
	}
	m.vp.SetHeight(20)
	m.vp.SetContent(strings.Repeat("hello world\n", 100))
	m.vp.SetYOffset(10)

	// header is 2 rows; screen row 2 → content line YOffset+0 = 10, col = x.
	if l, c := m.screenToContent(3, 2); l != 10 || c != 3 {
		t.Fatalf("(3,2) → (%d,%d), want (10,3)", l, c)
	}
	if l, _ := m.screenToContent(0, 5); l != 13 {
		t.Fatalf("row 5 → line %d, want 13", l)
	}
	// Column clamps to the line width.
	if _, c := m.screenToContent(999, 2); c != len("hello world") {
		t.Fatalf("col clamp → %d, want %d", c, len("hello world"))
	}
}

// selectedText returns the cell-precise plain text of the selection.
func TestSelectedText(t *testing.T) {
	m := newTestModel(t)
	m.contentPlain = []string{"alpha", "beta", "gamma", "delta"}

	// Single line, partial: "eta" from "beta" (cols 1..4).
	m.selAL, m.selAC, m.selHL, m.selHC = 1, 1, 1, 4
	if got := m.selectedText(); got != "eta" {
		t.Fatalf("partial single line = %q, want eta", got)
	}

	// Multi-line: from "lpha" (line0 col1) to "ga" (line2 col2).
	m.selAL, m.selAC, m.selHL, m.selHC = 0, 1, 2, 2
	if got := m.selectedText(); got != "lpha\nbeta\nga" {
		t.Fatalf("multi-line = %q, want %q", got, "lpha\nbeta\nga")
	}

	// Reversed drag yields the same.
	m.selAL, m.selAC, m.selHL, m.selHC = 2, 2, 0, 1
	if got := m.selectedText(); got != "lpha\nbeta\nga" {
		t.Fatalf("reversed = %q", got)
	}
}

// A click (no drag) does not produce a selection; a drag does.
func TestDragVsClick(t *testing.T) {
	m := newTestModel(t)
	m.contentPlain = make([]string, 50)
	m.vp.SetHeight(20)

	// Press without motion → release → no selection (selDragged stays false).
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 5})
	if !m.selecting {
		t.Fatal("click should begin a potential selection")
	}
	m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: 5})
	if m.selecting || m.selActive {
		t.Fatal("a plain click should not leave an active selection")
	}

	// Press, move, release → active selection.
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 5})
	m.handleMouse(tea.MouseMotionMsg{Button: tea.MouseLeft, Y: 9})
	if !m.selDragged {
		t.Fatal("motion should mark a drag")
	}
	m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, Y: 9})
	if !m.selActive {
		t.Fatal("a drag should leave an active selection")
	}
}
