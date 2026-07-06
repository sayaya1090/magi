package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// questOptionAt maps a click to an option row. A fresh model has vp.Height()=0 and
// no panes, so the box top is row 2 (header) and the first option is 3 rows below
// (box border + title + question) — row 5.
func TestQuestOptionAtGeometry(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.quest = &questReq{question: "pick", options: []string{"a", "b", "c"}}
	for i := range m.quest.options {
		if got, ok := m.questOptionAt(5 + i); !ok || got != i {
			t.Fatalf("click on option %d row: got=%d ok=%v", i, got, ok)
		}
	}
	if _, ok := m.questOptionAt(4); ok {
		t.Fatal("click above the options should miss")
	}
	if _, ok := m.questOptionAt(8); ok {
		t.Fatal("click below the last option should miss")
	}
}

// Tab/shift+tab cycle the selection (wrapping); up/down still clamp; enter answers
// the focused option and closes the modal; a number key answers directly.
func TestQuestKeyNav(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.quest = &questReq{question: "pick", options: []string{"a", "b", "c"}}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.quest.sel != 2 {
		t.Fatalf("after two tabs sel=%d, want 2", m.quest.sel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}) // wraps 2 → 0
	if m.quest.sel != 0 {
		t.Fatalf("tab should wrap to 0, sel=%d", m.quest.sel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // wraps 0 → 2
	if m.quest.sel != 2 {
		t.Fatalf("shift+tab should wrap to 2, sel=%d", m.quest.sel)
	}
	// enter answers the focused option (c) and closes the modal.
	if cmd, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil || m.quest != nil {
		t.Fatal("enter should answer and close the modal")
	}

	// A number key still answers directly.
	m.quest = &questReq{question: "pick", options: []string{"a", "b", "c"}}
	if cmd, _ := m.handleKey(tea.KeyPressMsg{Code: '2', Text: "2"}); cmd == nil || m.quest != nil {
		t.Fatal("number key should answer and close the modal")
	}
}

// A left-click on an option row focuses it (press) and picks it (release); a click
// off the rows is swallowed and leaves the modal open.
func TestQuestClickPicks(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.quest = &questReq{question: "pick", options: []string{"a", "b", "c"}}

	m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 6}) // option 1 row
	if m.quest == nil || m.quest.sel != 1 {
		t.Fatalf("press on option 1: sel=%v", m.quest)
	}
	cmd := m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 10, Y: 6})
	if cmd == nil || m.quest != nil {
		t.Fatalf("release on option 1 should pick it: cmd=%v quest=%v", cmd != nil, m.quest)
	}

	m.quest = &questReq{question: "pick", options: []string{"a", "b"}}
	if cmd := m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 1}); cmd != nil {
		t.Fatal("click off the options should be swallowed")
	}
	if m.quest == nil {
		t.Fatal("click off the options should leave the modal open")
	}
}
