package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

func ev(t *testing.T, ty event.Type, data any) event.Event {
	t.Helper()
	d, _ := json.Marshal(data)
	return event.Event{Type: ty, Data: d}
}

// Council events surface as transcript milestones plus a live header chip; the
// chip clears when the turn finishes.
func TestCouncilIndicator(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
	}))
	if m.councilRound != 1 {
		t.Fatalf("councilRound = %d, want 1", m.councilRound)
	}
	if last := m.blocks[len(m.blocks)-1]; last.kind != blockInfo || !strings.Contains(last.text, "round 1") || !strings.Contains(last.text, "Melchior") {
		t.Fatalf("convened not shown as a transcript line: %+v", last)
	}

	// Live polling updates the chip member.
	m.applyEvent(ev(t, event.TypeCouncilDeliberating, event.CouncilDeliberatingData{Round: 1, Member: "Balthasar", State: "asking"}))
	if m.councilMember != "Balthasar" {
		t.Fatalf("councilMember = %q, want Balthasar", m.councilMember)
	}

	// Each member's verdict surfaces with its reasoning (decision + lens + rationale),
	// and a continue verdict carries its next-step feedback, so the user can see WHY.
	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 1, Member: "Melchior", Lens: "correctness", Decision: "continue",
		Rationale: "the parser drops the trailing newline", Feedback: "handle EOF without a newline",
	}))
	v := m.blocks[len(m.blocks)-1]
	if v.kind != blockInfo ||
		!strings.Contains(v.text, "Melchior") ||
		!strings.Contains(v.text, "correctness") ||
		!strings.Contains(v.text, "the parser drops the trailing newline") ||
		!strings.Contains(v.text, "handle EOF without a newline") {
		t.Fatalf("verdict line missing member/lens/rationale/feedback: %q", v.text)
	}

	// The decision line shows the tally and that feedback was injected; polling clears.
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue),
		Tally:    council.Breakdown{Done: 1, Continue: 2},
		Feedback: "add the missing test",
	}))
	if m.councilMember != "" {
		t.Fatalf("councilMember should clear after a decision, got %q", m.councilMember)
	}
	last := m.blocks[len(m.blocks)-1]
	if !strings.Contains(last.text, "continue") || !strings.Contains(last.text, "1 done / 2 continue") || !strings.Contains(last.text, "feedback injected") {
		t.Fatalf("decided line missing tally/feedback: %q", last.text)
	}

	// Turn end clears the council indicator (chip disappears).
	m.applyEvent(event.Event{Type: event.TypeTurnFinished})
	if m.councilRound != 0 || m.councilMember != "" {
		t.Fatalf("council indicator should clear on turn finish: round=%d member=%q", m.councilRound, m.councilMember)
	}
}
