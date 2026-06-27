package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/prompt"
)

func pkey(m promptModel, s string) promptModel {
	r, _ := m.key(keyPress(s))
	return r.(promptModel)
}
func ptype(m promptModel, s string) promptModel {
	for _, ch := range s {
		r, _ := m.key(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = r.(promptModel)
	}
	return m
}

// The form collects text, select, confirm, and multiselect answers.
func TestPromptFormAnswers(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Title: "t", Fields: []prompt.Field{
		{Name: "note", Type: prompt.TypeNote, Label: "hi"},
		{Name: "user", Type: prompt.TypeText},
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"a", "b", "c"}},
		{Name: "ok", Type: prompt.TypeConfirm},
		{Name: "tags", Type: prompt.TypeMultiselect, Options: []string{"x", "y", "z"}},
	}})

	// First focusable skips the note → the text field.
	if m.sel != 1 {
		t.Fatalf("first focusable = %d, want 1 (text)", m.sel)
	}
	m = ptype(m, "alice")
	m = pkey(m, "down") // → select
	m = pkey(m, "right") // a→b
	m = pkey(m, "down")  // → confirm
	m = pkey(m, "right") // toggle to yes
	m = pkey(m, "down")  // → multiselect
	m = pkey(m, "space") // toggle x
	m = pkey(m, "right") // cursor → y
	m = pkey(m, "space") // toggle y

	a := m.answers()
	if a["user"] != "alice" {
		t.Errorf("user = %v", a["user"])
	}
	if a["mode"] != "b" {
		t.Errorf("mode = %v", a["mode"])
	}
	if a["ok"] != true {
		t.Errorf("ok = %v", a["ok"])
	}
	tags, _ := a["tags"].([]string)
	if len(tags) != 2 || tags[0] != "x" || tags[1] != "y" {
		t.Errorf("tags = %v", a["tags"])
	}
}

// Tab jumps to Submit; a number field rejects non-numeric input.
func TestPromptFormTabAndNumber(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "port", Type: prompt.TypeNumber},
	}})
	m = ptype(m, "80a8")    // 'a' rejected
	if m.state[0].buf != "808" {
		t.Errorf("number buf = %q, want 808", m.state[0].buf)
	}
	m = pkey(m, "tab")
	if m.sel != len(m.spec.Fields) {
		t.Errorf("Tab should jump to Submit, sel=%d", m.sel)
	}
}
