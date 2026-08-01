package tui

import (
	"fmt"
	"regexp"
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

// Every surface that reserves rows, at every height from the floor up, checked deterministically.
//
// The walk reaches these by chance: it has to draw the size AND open the surface AND still be there
// when the frame is measured, and until 4733e24 its floor was eight rows so 7 was never drawn at
// all. That gap is what hid the profile form's two-line last resort. A grid does not depend on
// luck, and it names which surface at which height rather than a seed and a step number.
//
// Seven is the floor, measured: chrome is 6 irreducible and each of these adds one. Six is left out
// deliberately — every modal overflows it by exactly one row, and closing that would mean dropping
// the footer or the input's border.
func TestEverySurfaceFitsFromTheFloorUp(t *testing.T) {
	applyTheme(true)
	surfaces := []struct {
		what string
		open func(s *script)
	}{
		{"nothing", func(s *script) {}},
		{"find bar", func(s *script) { s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}) }},
		{"palette", func(s *script) { s.typeText("/") }},
		{"permission", func(s *script) {
			s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
				CallID: "cx", Name: "bash", Args: []byte(`{"command":"a rather long command line"}`), Reason: "policy"})
		}},
		{"question", func(s *script) {
			s.m.quest = &questReq{question: "which of these approaches should it take?",
				options: []string{"the first one", "the second one", "a third"}}
		}},
		{"route editor", func(s *script) { s.m.openRouteEditor() }},
		// Opened the way the app opens it — through the route editor — because the form's rows are
		// reserved inside the routing branch, and calling openProfileForm alone leaves m.routing
		// false: the surface is on screen and reserves nothing, which is a grid cell that checks
		// a state the app cannot be in. (Found while writing this: the cell passed under a
		// mutation that reintroduced the two-line last resort.)
		{"profile form", func(s *script) {
			s.m.openRouteEditor()
			s.m.routeSel = len(s.m.routeList) - 1 // "+ add profile"
			s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		}},
		{"profile form, editing", func(s *script) {
			s.m.openRouteEditor()
			s.m.routeSel = len(s.m.routeList) - 1
			s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
			if s.m.profileForm != nil {
				s.m.profileForm.editing = true
				s.m.profileForm.buf = "https://a-fairly-long-endpoint.example.com/v1"
			}
		}},
		{"resume picker", func(s *script) { s.m.resuming = true }},
	}
	for _, h := range []int{7, 8, 9, 10, 14} {
		for _, w := range []int{24, 40, 80} {
			for _, sf := range surfaces {
				s := newScript(t)
				s.steer("r1", "a question")
				s.assistantText("an answer with enough text to fill a line or two")
				s.send(tea.WindowSizeMsg{Width: w, Height: h})
				sf.open(s)
				rows := strings.Split(s.rawView(), "\n")
				if len(rows) > h {
					t.Errorf("%dx%d %s: the frame is %d rows (chrome=%d modalRoom=%d vp=%d)\n%s",
						w, h, sf.what, len(rows), s.m.chromeHeight(), s.m.modalRoom(), s.m.vp.Height(),
						strings.Join(rows, "\n"))
				}
				worst, worstW := "", 0
				for _, r := range rows {
					if x := lipgloss.Width(strings.TrimRight(stripANSI(r), " ")); x > worstW {
						worst, worstW = r, x
					}
				}
				if worstW > w {
					t.Errorf("%dx%d %s: widest row is %d cells: %q", w, h, sf.what, worstW, stripANSI(worst))
				}
			}
		}
	}
}

// A surface that windows its rows must SAY it did. A list silently ending at its fourth entry
// reads as a magi with four of them — the defect fixed one surface at a time all through
// 2026-08-01 (the palette, the worker panel, the route editor, the question modal), each found by
// hand after the last. Nothing held the whole set at once, so the next one added would be found
// the same way.
//
// Each case gives its surface more items than can fit and asserts two things: that it really shed
// (or the case proves nothing), and that what it drew carries a mark.
func TestAWindowedSurfaceSaysItCutSomething(t *testing.T) {
	applyTheme(true)
	// A marker is any of the shapes these surfaces use: "n/N", "… N more", "… N more rows".
	marked := func(s string) bool {
		plain := stripANSI(s)
		return strings.Contains(plain, "more") || regexp.MustCompile(`\d+/\d+`).MatchString(plain)
	}
	for _, tc := range []struct {
		what  string
		items int
		open  func(s *script)
		view  func(m *Model) string
	}{
		{"question options", 12, func(s *script) {
			opts := make([]string, 12)
			for i := range opts {
				opts[i] = fmt.Sprintf("option number %d", i)
			}
			s.m.quest = &questReq{question: "which one?", options: opts}
		}, func(m *Model) string { return m.questView() }},
		{"resume picker", 20, func(s *script) {
			s.m.resuming = true
			for i := 0; i < 20; i++ {
				s.m.resumeList = append(s.m.resumeList, session.SessionMeta{ID: session.SessionID(fmt.Sprintf("s%02d", i)), Title: fmt.Sprintf("session number %d", i)})
			}
		}, func(m *Model) string { return m.resumeView() }},
		{"route editor", 14, func(s *script) {
			s.m.openRouteEditor()
			for i := 0; i < 14; i++ {
				s.m.routeList = append(s.m.routeList, routeRow{kind: rowProfile,
					name: fmt.Sprintf("profile:p%02d", i), value: "endpoint · model"})
			}
		}, func(m *Model) string { return m.routeView() }},
	} {
		t.Run(tc.what, func(t *testing.T) {
			s := newScript(t)
			s.send(tea.WindowSizeMsg{Width: 60, Height: 12})
			tc.open(s)
			out := tc.view(&s.m)
			if lipgloss.Height(out) >= tc.items {
				t.Fatalf("nothing was cut, so this asserts nothing (%d lines for %d items)",
					lipgloss.Height(out), tc.items)
			}
			if !marked(out) {
				t.Errorf("%d items shown in %d lines with no mark:\n%s",
					tc.items, lipgloss.Height(out), stripANSI(out))
			}
		})
	}
}
