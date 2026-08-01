package tui

import (
	"testing"

	"github.com/sayaya1090/magi/internal/prompt"
)

// ←/→ move through a select's options and wrap at both ends. Nothing exercised that: the vertical
// keys go through a different path, so wrap() — the four call sites that decide what happens when
// the cursor walks off either end — was never once entered by a test. An off-by-one there parks the
// cursor on a phantom option or refuses to move, on the form a plugin login is answered through.
func TestASelectWrapsAtBothEnds(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "mode", Type: prompt.TypeSelect, Options: []string{"a", "b", "c"}},
	}})
	if m.state[0].optIdx != 0 {
		t.Fatalf("optIdx starts at %d, not 0 — the walk below assumes it", m.state[0].optIdx)
	}
	m = pkey(m, "left") // off the front, round to the last
	if got := m.state[0].optIdx; got != 2 {
		t.Errorf("left from the first option = %d, want 2", got)
	}
	m = pkey(m, "right") // off the back, round to the first
	if got := m.state[0].optIdx; got != 0 {
		t.Errorf("right from the last option = %d, want 0", got)
	}
	// …and the answer follows the cursor, so a wrap that moved the index without moving the
	// selection would be worse than not moving at all.
	m = pkey(m, "left")
	if got := m.answers()["mode"]; got != "c" {
		t.Errorf("after wrapping to the last option the answer is %v, want c", got)
	}
}

// A multiselect uses the same helper for its sub-cursor, and the checkbox it toggles is indexed by
// it — a sub-cursor out of range is a space keypress that ticks nothing or panics.
func TestAMultiselectSubCursorWrapsAndStaysInRange(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "opts", Type: prompt.TypeMultiselect, Options: []string{"x", "y"}},
	}})
	for i, key := range []string{"left", "left", "right", "right", "right"} {
		m = pkey(m, key)
		if got := m.state[0].subIdx; got < 0 || got >= 2 {
			t.Fatalf("step %d (%s): subIdx = %d, outside 0..1", i, key, got)
		}
	}
	// Space ticks whatever the cursor is on, wherever it wrapped to.
	// "space", not " ": the shared keyPress helper maps names, and an unmapped string arrives as
	// key code 0 — a keypress that matches nothing and silently does nothing.
	at := m.state[0].subIdx
	m = pkey(m, "space")
	if !m.state[0].checks[at] {
		t.Errorf("space did not tick the option the cursor was on (%d)", at)
	}
}

// The degenerate spec: a select with no options at all. wrap divides the walk by the option count,
// and a form built from a plugin's manifest can carry an empty list.
func TestASelectWithNoOptionsDoesNotMoveOrPanic(t *testing.T) {
	applyTheme(true)
	m := newPromptModel(prompt.Spec{Fields: []prompt.Field{
		{Name: "mode", Type: prompt.TypeSelect, Options: nil},
	}})
	for _, key := range []string{"left", "right", "left"} {
		m = pkey(m, key)
		if got := m.state[0].optIdx; got != 0 {
			t.Fatalf("%s on an empty select moved the cursor to %d", key, got)
		}
	}
	_ = m.body() // it still draws
}
