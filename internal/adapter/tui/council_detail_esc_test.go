package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
)

// esc leaves the council verdict detail "like exiting zoom" — and exiting zoom refreshes.
// This path did not: it dropped the modal and returned, leaving the viewport (and the
// contentLines that selection, copy and search read) holding the detail the user had just
// left. The header went back to the transcript while the body still showed the verdict.
//
// Both mouse dismissals refresh; only the keyboard one did not.
func TestEscLeavingCouncilDetailRestoresTheTranscript(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 100, 30
	m.ready = true
	m.blocks = []block{
		{kind: blockUser, text: "a question the transcript holds", reqID: "r1"},
		{kind: blockAssistant, text: "and the answer below it"},
	}
	m.councilDetail = &event.CouncilVerdictData{
		Member: "Melchior", Lens: "correctness", Decision: "done",
		Rationale: "the detail body that must not outlive its modal",
	}
	m.refresh()
	if !strings.Contains(strings.Join(m.contentPlain, "\n"), "Melchior") {
		t.Fatalf("the detail should be on screen before esc:\n%s", strings.Join(m.contentPlain, "\n"))
	}

	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !handled {
		t.Fatal("esc should be handled while the council detail is open")
	}
	if m.councilDetail != nil {
		t.Fatal("esc should close the council detail")
	}

	shown := strings.Join(m.contentPlain, "\n")
	if strings.Contains(shown, "Melchior") {
		t.Errorf("the closed detail is still what the viewport holds:\n%s", shown)
	}
	if !strings.Contains(shown, "a question the transcript holds") {
		t.Errorf("the transcript should be back:\n%s", shown)
	}
}
