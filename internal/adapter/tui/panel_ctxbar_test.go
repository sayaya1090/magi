package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The panel draws how full the window is, not only the footer's sentence.
//
// The words have been in the footer since it existed, and words are what you read when you go
// looking. A bar is what you see without looking — which is what matters about a number you want
// to notice BEFORE it forces a compaction.
func TestThePanelDrawsHowFullTheContextWindowIs(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 160, 48
	m.app.SetTodos(m.sid, []session.Todo{{Content: "keep the panel up", Status: "pending"}})
	m.ctxPct, m.ctxTokens, m.ctxWindow = 42, 55000, 131000

	box, _, _, ok := m.floatPanel()
	if !ok {
		t.Fatal("the panel did not draw")
	}
	if !strings.Contains(box, "Context") {
		t.Errorf("the panel does not say what the section is:\n%s", box)
	}
	if !strings.Contains(box, "━") {
		t.Errorf("the panel states the number and does not draw it:\n%s", box)
	}
	// The numbers stay: a bar says roughly, and roughly is not enough to decide whether to compact.
	if !strings.Contains(box, "42%") {
		t.Errorf("the bar replaced the numbers instead of joining them:\n%s", box)
	}
}

// Before anything has reported usage there is nothing to draw, and nothing is drawn.
func TestThePanelDrawsNoContextBarBeforeAnythingHasBeenMeasured(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 160, 48
	m.app.SetTodos(m.sid, []session.Todo{{Content: "keep the panel up", Status: "pending"}})

	box, _, _, ok := m.floatPanel()
	if !ok {
		t.Fatal("the panel did not draw")
	}
	if strings.Contains(box, "Context") {
		t.Errorf("a context section appeared with no measurement behind it:\n%s", box)
	}
}

// An unknown window has no denominator, so it says the size and draws no bar rather than
// inventing one to fill.
func TestAnUnknownWindowIsSaidAndNotDrawn(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 160, 48
	m.app.SetTodos(m.sid, []session.Todo{{Content: "keep the panel up", Status: "pending"}})
	m.ctxTokens, m.ctxWindow = 55000, 0

	box, _, _, _ := m.floatPanel()
	if !strings.Contains(box, "ctx ~") {
		t.Errorf("an unknown window says nothing at all:\n%s", box)
	}
	if strings.Contains(box, "━") {
		t.Errorf("a bar was drawn against a denominator nobody knows:\n%s", box)
	}
}
