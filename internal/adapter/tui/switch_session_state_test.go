package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

// Resuming another session replaces everything on screen that belonged to the old one — the
// transcript, the cache, the history, the live text. Two things were left behind, and both of
// them state something about the session the user just left while the header says the new one.
//
// The council detail is the loud one: a full-screen panel with another session's member,
// rationale and evidence, sitting over the resumed transcript with nothing marking it as foreign.
// It is reachable the ordinary way, which this test walks — the input keeps taking keys while the
// panel is up, so `/resume` typed there switches the session underneath it.
func TestResumingAnotherSessionClosesWhatBelongedToTheOldOne(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.assistantText("session A said this")
	if _, err := s.m.app.CreateSession(context.Background(), command.CreateSession{Workdir: s.m.workdir}); err != nil {
		t.Fatal(err)
	}
	s.m.councilDetail = &event.CouncilVerdictData{
		Round: 1, Member: "Melchior", Decision: "done", Lens: "correctness",
		Rationale: "the rationale from session A",
	}
	s.m.councilDetailEvidence = "the evidence from session A"
	s.m.selAL, s.m.selAC, s.m.selHL, s.m.selHC = 0, 0, 0, 5
	s.m.selActive = true
	s.m.refresh()

	before := s.m.sid
	s.typeText("/resume 1").enter()
	if s.m.sid == before {
		t.Fatal("the session did not switch, so nothing here was tested")
	}

	if s.m.councilDetail != nil {
		t.Error("the resumed session is showing the previous session's council verdict")
	}
	if s.m.councilDetailEvidence != "" {
		t.Errorf("the previous session's evidence is still loaded: %q", s.m.councilDetailEvidence)
	}
	if s.m.selActive || s.m.selecting {
		t.Error("a selection made in the previous session is still highlighted in this one")
	}
	frame := ansiSeq.ReplaceAllString(s.rawView(), "")
	for _, gone := range []string{"Melchior", "session A", "rationale from session A"} {
		if strings.Contains(frame, gone) {
			t.Errorf("%q is still on screen after resuming another session:\n%s", gone, frame)
		}
	}
}

// The switch clears them; it does not break them. A verdict opened in the session you are ON
// still opens, which is the whole point of the panel.
func TestACouncilDetailStillOpensInTheSessionItBelongsTo(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.m.councilDetail = &event.CouncilVerdictData{
		Round: 1, Member: "Balthasar", Decision: "continue", Lens: "correctness",
		Rationale: "this rationale must be readable",
	}
	s.m.refresh()
	if frame := ansiSeq.ReplaceAllString(s.rawView(), ""); !strings.Contains(frame, "Balthasar") {
		t.Errorf("the panel does not render its own session's verdict:\n%s", frame)
	}
}
