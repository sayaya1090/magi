package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
)

// askWith opens the selection modal on a sized terminal. It goes through the event the app
// actually emits rather than assigning m.quest, so the modal under test is the one a run produces.
func askWith(t *testing.T, w, h, n int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	opts := make([]string, n)
	for i := range opts {
		opts[i] = fmt.Sprintf("option %02d", i)
	}
	s.emit(event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID: "c1#1", Question: "which approach?", Options: opts,
	})
	if s.m.quest == nil {
		t.Fatal("the modal did not open, so nothing here is under test")
	}
	return s
}

// The option being chosen has to be on screen. A short terminal trims the option list, and the
// trim always kept the FIRST few — so arrowing down to the seventh choice showed six others and
// not the one about to be answered. The palette next door was fixed for exactly this; the modal
// the user has to answer was not.
func TestTheChosenOptionStaysOnScreen(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{80, 14, 10}, {80, 12, 8}, {60, 15, 12}} {
		s := askWith(t, c.w, c.h, c.n)
		for i := 0; i < c.n-1; i++ {
			s.send(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		if s.m.quest.sel != c.n-1 {
			t.Fatalf("w=%d h=%d: selection is %d, not the last option", c.w, c.h, s.m.quest.sel)
		}
		out := ansiSeq.ReplaceAllString(s.m.questView(), "")
		want := fmt.Sprintf("%d. option %02d", c.n, c.n-1)
		if !strings.Contains(out, want) {
			t.Errorf("w=%d h=%d n=%d: the selected %q is not drawn:\n%s", c.w, c.h, c.n, want, out)
		}
	}
}

// A trimmed list says it was trimmed. Options are numbered, and a modal that ends at "3." with
// nothing said reads as a question with three answers — the user picks from what they can see.
func TestATrimmedQuestionSaysOptionsAreHidden(t *testing.T) {
	applyTheme(true)
	s := askWith(t, 80, 12, 9)
	out := ansiSeq.ReplaceAllString(s.m.questView(), "")
	if strings.Contains(out, "9. option 08") {
		t.Skip("everything fits at this size; the cut is not under test")
	}
	if !strings.Contains(out, "more") {
		t.Errorf("options were dropped with nothing saying so:\n%s", out)
	}
}

// …and a modal that fits carries no marker and every option.
func TestAQuestionThatFitsIsUntouched(t *testing.T) {
	applyTheme(true)
	s := askWith(t, 80, 40, 5)
	out := ansiSeq.ReplaceAllString(s.m.questView(), "")
	for i := 0; i < 5; i++ {
		if !strings.Contains(out, fmt.Sprintf("%d. option %02d", i+1, i)) {
			t.Errorf("option %d is missing from a modal with room for all of them:\n%s", i+1, out)
		}
	}
	if strings.Contains(out, "more") {
		t.Errorf("a modal that fits is marked as cut:\n%s", out)
	}
}

// Whatever it drops, it stays inside the terminal — the reason the trim exists at all.
func TestTheQuestionModalFitsItsTerminal(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{80, 14, 10}, {80, 12, 8}, {60, 15, 12}, {100, 30, 6}} {
		s := askWith(t, c.w, c.h, c.n)
		s.m.quest.sel = c.n - 1
		if got := lipgloss.Height(s.m.questView()); got > s.m.modalRoom() {
			t.Errorf("w=%d h=%d n=%d: the modal draws %d rows into %d of room",
				c.w, c.h, c.n, got, s.m.modalRoom())
		}
	}
}
