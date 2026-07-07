package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/prompt"
	"github.com/sayaya1090/magi/internal/version"
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

// The form collects text, confirm, multiselect, and select answers. Options are laid
// out vertically, so ↑/↓ walks the options of a list before crossing to the next field.
func TestPromptFormAnswers(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Title: "t", Fields: []prompt.Field{
		{Name: "note", Type: prompt.TypeNote, Label: "hi"},
		{Name: "user", Type: prompt.TypeText},
		{Name: "ok", Type: prompt.TypeConfirm},
		{Name: "tags", Type: prompt.TypeMultiselect, Options: []string{"x", "y", "z"}},
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"a", "b", "c"}},
	}})

	// First focusable skips the note → the text field.
	if m.sel != 1 {
		t.Fatalf("first focusable = %d, want 1 (text)", m.sel)
	}
	m = ptype(m, "alice")
	m = pkey(m, "down")  // → confirm
	m = pkey(m, "right") // toggle to yes
	m = pkey(m, "down")  // → multiselect, cursor on x
	m = pkey(m, "space") // check x
	m = pkey(m, "down")  // cursor x→y (within the list)
	m = pkey(m, "space") // check y
	m = pkey(m, "down")  // cursor y→z (still within the list)
	m = pkey(m, "down")  // z is last → cross to select, optIdx=a
	m = pkey(m, "down")  // a→b (select cursor is the selection)

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

// A select's ↑/↓ moves within its options and only crosses to Submit past the last
// option. Enter on the last field submits in one press (no parking on Submit).
func TestPromptSelectVerticalNav(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"a", "b", "c"}},
	}})
	if m.sel != 0 {
		t.Fatalf("sel = %d, want 0 (select)", m.sel)
	}
	m = pkey(m, "down") // a→b, stays on the field
	if m.sel != 0 || m.state[0].optIdx != 1 {
		t.Fatalf("after down: sel=%d optIdx=%d, want 0/1", m.sel, m.state[0].optIdx)
	}
	// Enter on the sole select field submits directly (single Enter, not two).
	r, cmd := m.key(keyPress("enter"))
	m = r.(promptModel)
	if cmd == nil {
		t.Errorf("Enter on the last select should submit (return a Quit cmd), got nil")
	}
	if m.answers()["mode"] != "b" {
		t.Errorf("mode = %v, want b", m.answers()["mode"])
	}
}

// In a multi-field form, Enter on a non-last select advances to the next input
// field rather than submitting.
func TestPromptSelectEnterAdvancesMidForm(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"a", "b"}},
		{Name: "note", Type: prompt.TypeText},
	}})
	if m.sel != 0 {
		t.Fatalf("sel = %d, want 0 (select)", m.sel)
	}
	r, cmd := m.key(keyPress("enter"))
	m = r.(promptModel)
	if cmd != nil {
		t.Errorf("Enter on a non-last select must not submit, got a cmd")
	}
	if m.sel != 1 {
		t.Errorf("Enter should advance to the text field, sel=%d want 1", m.sel)
	}
}

// The logo banner and the vertical options both render (no panic, expected glyphs).
func TestPromptViewRendersLogoAndVerticalOptions(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"first", "second"}},
	}})
	out := m.View().Content
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("options missing from view")
	}
	if !strings.Contains(out, "●") || !strings.Contains(out, "○") {
		t.Errorf("select markers missing: %q", out)
	}
	// logoBlock renders the wordmark; its version line is always present.
	if !strings.Contains(out, version.String()) {
		t.Errorf("logo/version banner missing from prompt view")
	}
}

// Tab jumps to Submit; a number field rejects non-numeric input.
func TestPromptFormTabAndNumber(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "port", Type: prompt.TypeNumber},
	}})
	m = ptype(m, "80a8") // 'a' rejected
	if m.state[0].buf != "808" {
		t.Errorf("number buf = %q, want 808", m.state[0].buf)
	}
	m = pkey(m, "tab")
	if m.sel != len(m.spec.Fields) {
		t.Errorf("Tab should jump to Submit, sel=%d", m.sel)
	}
}
