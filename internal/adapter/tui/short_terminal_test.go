package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The input box grows to maxInputRows and never asked the terminal, so a six-line draft on an
// eight-row screen drew a box eight rows tall before the header was counted — and on an alt screen
// the header and the transcript are then off the display. Pasting a few lines into a split pane is
// the whole reproduction.
func TestTheInputBoxFitsAShortTerminal(t *testing.T) {
	applyTheme(true)
	for _, h := range []int{8, 9, 10, 12, 14, 30} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 60, Height: h})
		s.assistantText("some work happened")
		for i := 0; i < 8; i++ {
			s.typeText("a line of draft")
			s.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		}
		if rows := len(strings.Split(s.rawView(), "\n")); rows > h {
			t.Errorf("h=%d: an eight-line draft draws a %d-row frame", h, rows)
		}
		// …and the box never shrinks below one usable row, whatever the screen.
		if got := s.m.ta.Height(); got < 1 {
			t.Errorf("h=%d: the input box is %d rows — there is nowhere to type", h, got)
		}
	}
}

// A short terminal with a modal open is the tightest case: the modal is the thing that has to be
// read and answered, and the input is how it gets answered.
func TestAModalAndADraftBothFitAShortTerminal(t *testing.T) {
	applyTheme(true)
	for _, h := range []int{8, 10, 12} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 50, Height: h})
		for i := 0; i < 6; i++ {
			s.typeText("draft")
			s.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		}
		s.emit(event.TypeQuestionRequested, event.QuestionRequestedData{
			CallID: "c1#1", Question: "which approach?", Options: []string{"one", "two", "three"}})
		if rows := len(strings.Split(s.rawView(), "\n")); rows > h {
			t.Errorf("h=%d: modal plus draft draws a %d-row frame", h, rows)
		}
	}
}

// Switching sessions leaves the turn's identity behind with everything else that belongs to the
// session being left. It did not: running was cleared but turnReqID still pointed at a request in
// the transcript that had just been replaced, and the revive path only adopts a block's reqID when
// turnReqID is EMPTY — so a stale one survived it and the spinner hunted for a block that is not in
// this session.
func TestSwitchingSessionsDropsTheOldTurnsIdentity(t *testing.T) {
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 80, Height: 30})
	s.typeText("first request").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "first request"}}})
	if s.m.turnReqID == "" {
		t.Fatal("no turn is running, so the switch is not under test")
	}

	other := makeSessions(t, s, 1)[0]
	s.m.switchSession(other)

	if s.m.turnReqID != "" {
		t.Errorf("the resumed session carries the old turn's reqID %q", s.m.turnReqID)
	}
	if s.m.awaitingTurnReqID {
		t.Error("the resumed session is still waiting for the other session's prompt")
	}
	if !s.m.turnStart.IsZero() {
		t.Error("the resumed session's meter would count from the other session's start")
	}
	// And once engine activity revives the spinner here, it attaches to a block on screen —
	// the path that only adopts a reqID when turnReqID is empty.
	s.emit(event.TypePartDelta, event.PartDeltaData{
		MessageID: "m", PartID: "p", Kind: session.PartText, Text: "tok "})
	if s.m.running && s.m.turnReqID != "" {
		found := false
		for _, b := range s.m.blocks {
			if b.reqID == s.m.turnReqID {
				found = true
			}
		}
		if !found {
			t.Errorf("the spinner is attached to %q, which is not in this transcript", s.m.turnReqID)
		}
	}
}

// The picker's header is 57 cells whatever the terminal is, and its rows have an eleven-cell age
// column plus a twenty-cell title floor — so a narrow screen overflows on the floor alone. One
// over-wide row pads every other row in the joined frame out to match it.
func TestTheSessionPickerFitsANarrowTerminal(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{20, 24, 30, 40, 56} {
		s := openPicker(t, w, 20, 14)
		for i, l := range strings.Split(s.m.resumeView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("w=%d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
}

// Same for the routing editor's rows: the name column alone is sixteen cells, and the header was
// clipped long before the rows under it were.
func TestTheRouteEditorRowsFitANarrowTerminal(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{20, 24, 30, 40} {
		s := openRoute(t, w, 24)
		for i, l := range strings.Split(s.m.routeView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("w=%d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
}
