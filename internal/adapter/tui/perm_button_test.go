package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The click hit-test derives the button row from permModalHeight(); if the box
// renders a different number of rows the math is off by that much, so pin the two
// together for both the plain and the reason-carrying modal.
func TestPermModalHeightMatchesRender(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	for _, reason := range []string{"", "network egress command"} {
		m.perm = &permReq{name: "bash", args: `{}`, reason: reason}
		if got, want := lipgloss.Height(m.permView()), m.permModalHeight(); got != want {
			t.Fatalf("reason=%q: rendered %d rows, permModalHeight()=%d", reason, got, want)
		}
	}
}

// permButtonAt maps a click on the button row to the right button, and rejects
// clicks off that row. Geometry: a fresh model has vp.Height()=0 and no panes,
// so the modal's top is row 2 (header) and the buttons are modalHeight-3 below —
// row 5 with no reason. Content starts at screen X=3 (border 1 + padding 2).
func TestPermButtonAtGeometry(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.perm = &permReq{name: "bash", args: `{}`}
	const row = 5

	// Left edge of each pill (x=3, then + widths+gaps) must resolve to its index.
	cx := 3
	for want, b := range permButtons() {
		if got, ok := m.permButtonAt(cx+1, row); !ok || got != want {
			t.Fatalf("click on %q pill: got=%d ok=%v, want %d", b.word, got, ok, want)
		}
		cx += permButtonWidth(b) + 1
	}
	// A click one row off the button row is not on any button.
	if _, ok := m.permButtonAt(4, row-1); ok {
		t.Fatal("click above the button row should miss")
	}
	if _, ok := m.permButtonAt(4, row+1); ok {
		t.Fatal("click below the button row should miss")
	}
	// A click past the last pill misses.
	if _, ok := m.permButtonAt(200, row); ok {
		t.Fatal("click past the pills should miss")
	}
}

// Tab/shift+tab cycle focus; enter activates the focused button; the direct
// hotkeys still fire; esc denies. respond() clears m.perm, so that's the signal
// the choice was taken (its returned cmd only touches the app when executed).
func TestPermButtonKeyNav(t *testing.T) {
	applyTheme(true)
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

	m := &Model{width: 80}
	m.perm = &permReq{name: "bash", args: `{}`}
	// Two tabs: allow(0) → always(1) → project(2).
	m.handleKey(tab)
	m.handleKey(tab)
	if m.perm.sel != 2 {
		t.Fatalf("after two tabs sel=%d, want 2", m.perm.sel)
	}
	// shift+tab wraps back to always(1).
	m.handleKey(shiftTab)
	if m.perm.sel != 1 {
		t.Fatalf("after shift+tab sel=%d, want 1", m.perm.sel)
	}
	// enter activates the focused button (always) and closes the modal.
	if cmd, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter on a focused button should return a respond cmd")
	}
	if m.perm != nil {
		t.Fatalf("enter should close the modal, perm=%+v", m.perm)
	}

	// A direct hotkey still works regardless of focus.
	m.perm = &permReq{name: "bash", args: `{}`, sel: 2}
	if cmd, _ := m.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"}); cmd == nil || m.perm != nil {
		t.Fatal("hotkey n should deny and close the modal")
	}
}

// A left-click on a pill moves focus (press) and activates it (release).
func TestPermButtonClickActivates(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.perm = &permReq{name: "bash", args: `{}`}

	// Press on the "project" pill (index 2) focuses it without closing the modal.
	cx := 3
	for i := 0; i < 2; i++ {
		cx += permButtonWidth(permButtons()[i]) + 1
	}
	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: cx + 1, Y: 5})
	if m.perm == nil || m.perm.sel != 2 {
		t.Fatalf("press on project pill: sel=%v", m.perm)
	}
	// Release on the same pill activates it and closes the modal.
	cmd := m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: cx + 1, Y: 5})
	if cmd == nil || m.perm != nil {
		t.Fatalf("release on project pill should activate: cmd=%v perm=%v", cmd != nil, m.perm)
	}

	// A click off the button row is swallowed (no transcript selection) and leaves
	// the modal open.
	m.perm = &permReq{name: "bash", args: `{}`}
	if cmd := m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: 1}); cmd != nil {
		t.Fatal("click off the buttons should be swallowed")
	}
	if m.perm == nil {
		t.Fatal("click off the buttons should leave the modal open")
	}
}
