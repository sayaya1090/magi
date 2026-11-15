package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A council is one line in the transcript, accepted or not, and its members open when that line
// is clicked (asked 2026-11-15). A council still deliberating is not folded: there is no line yet
// to stand for it.
func TestACouncilFoldsToOneLineAndOpensOnClick(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}}))
	for _, who := range []string{"Melchior", "Balthasar", "Casper"} {
		m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: who, Decision: "continue", Rationale: who + "-REASON"}))
	}
	if live := ansi.Strip(m.transcript()); !strings.Contains(live, "Melchior") {
		t.Fatalf("a council still deliberating shows its members as they land:\n%s", live)
	}
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{Round: 1, Decision: "continue"}))
	shut := ansi.Strip(m.transcript())
	if !strings.Contains(shut, "council: not accepted") {
		t.Errorf("the decided council is one line saying it was not accepted:\n%s", shut)
	}
	for _, gone := range []string{"Melchior", "Casper-REASON", "council round 1"} {
		if strings.Contains(shut, gone) {
			t.Errorf("folded council still shows %q:\n%s", gone, shut)
		}
	}
	last := len(m.blocks) - 1
	if !m.toggleCouncilAt(m.blockLineStart[last]) {
		t.Fatal("clicking the council line did not open it")
	}
	open := ansi.Strip(m.transcript())
	for _, back := range []string{"Melchior", "Casper", "council round 1"} {
		if !strings.Contains(open, back) {
			t.Errorf("opened council lacks %q:\n%s", back, open)
		}
	}
	if !m.toggleCouncilAt(m.blockLineStart[last]) || strings.Contains(ansi.Strip(m.transcript()), "Melchior") {
		t.Error("a second click folds it again")
	}
}
