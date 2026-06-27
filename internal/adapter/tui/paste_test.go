package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
)

func newPasteModel() *Model {
	applyTheme(true)
	ta := textarea.New()
	return &Model{ta: ta}
}

// A large multiline paste collapses to a placeholder; the input shows the chip,
// not the raw content.
func TestHandlePasteCollapses(t *testing.T) {
	m := newPasteModel()
	blob := strings.Repeat("line\n", 30)
	m.handlePaste(blob)
	val := m.ta.Value()
	if !pasteRE.MatchString(val) {
		t.Fatalf("input should hold a placeholder, got %q", val)
	}
	if strings.Contains(val, "line\nline") {
		t.Fatal("raw multiline content should not be in the input")
	}
	if len(m.pastes) != 1 {
		t.Fatalf("expected 1 stored paste, got %d", len(m.pastes))
	}
}

// A short single-line paste is inserted inline (no placeholder).
func TestHandlePasteInline(t *testing.T) {
	m := newPasteModel()
	m.handlePaste("just a short bit")
	if pasteRE.MatchString(m.ta.Value()) {
		t.Fatal("short paste should be inline, not collapsed")
	}
	if m.ta.Value() != "just a short bit" {
		t.Fatalf("input = %q", m.ta.Value())
	}
}

// expandPastes restores the full content for the agent.
func TestExpandPastes(t *testing.T) {
	m := newPasteModel()
	blob := "alpha\nbeta\ngamma\n" + strings.Repeat("x", 300)
	m.handlePaste(blob)
	placeholder := m.ta.Value()

	full := "see this: " + placeholder
	got := m.expandPastes(full)
	if !strings.Contains(got, "alpha\nbeta\ngamma") {
		t.Fatalf("expandPastes did not restore content: %q", got)
	}
	if strings.Contains(got, "pasted") {
		t.Fatalf("placeholder should be gone after expand: %q", got)
	}
}

// On submit the transcript shows the FULL pasted content (expandPastes), while
// the input chip stays compact.
func TestPasteExpandsForDisplay(t *testing.T) {
	m := newPasteModel()
	blob := strings.Repeat("row\n", 10)
	m.handlePaste(blob)
	ph := m.ta.Value() // the [#N pasted L lines] chip
	if !pasteRE.MatchString(ph) {
		t.Fatalf("input should hold a chip, got %q", ph)
	}
	if !strings.Contains(m.expandPastes(ph), "row\nrow") {
		t.Fatal("expandPastes should restore full content for the transcript")
	}
}
