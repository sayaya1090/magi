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
		Task: "fix the parser bug", Report: "fixed the EOF handling",
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

	// A verdict becomes a COMPACT block that carries the full vote data; the detail
	// (lens/rationale/feedback) shows only in the modal opened by clicking it.
	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 1, Member: "Melchior", Lens: "correctness", Decision: "continue",
		Rationale: "the parser drops the trailing newline", Feedback: "handle EOF without a newline",
	}))
	v := m.blocks[len(m.blocks)-1]
	if v.kind != blockCouncilVerdict || v.councilVerdict == nil {
		t.Fatalf("verdict should be a compact council block carrying its data, got kind=%d", v.kind)
	}
	if v.councilVerdict.Member != "Melchior" || v.councilVerdict.Rationale == "" {
		t.Fatalf("verdict block should carry the full vote data: %+v", v.councilVerdict)
	}
	// The compact render shows member + decision but NOT the rationale. A termination
	// "continue" is shown as "reject" (it's a rejection, not progress).
	m.width = 80
	compact := m.renderBlock(v)
	if !strings.Contains(compact, "Melchior") || !strings.Contains(compact, "reject") {
		t.Fatalf("compact line should show member + reject (termination continue): %q", compact)
	}
	if strings.Contains(compact, "continue") {
		t.Fatalf("termination verdict should read 'reject', not 'continue': %q", compact)
	}
	if strings.Contains(compact, "trailing newline") {
		t.Fatalf("compact line must NOT include the rationale: %q", compact)
	}
	// Clicking opens the full-screen detail with the full reasoning AND the round's
	// evidence (what the members saw): task + report flow through convened→verdict.
	if v.evidence == "" || !strings.Contains(v.evidence, "fix the parser bug") {
		t.Fatalf("verdict block should carry the round's evidence, got %q", v.evidence)
	}
	m.councilDetail = v.councilVerdict
	m.councilDetailEvidence = v.evidence
	detail := m.renderCouncilDetail(80)
	if !strings.Contains(detail, "correctness") ||
		!strings.Contains(detail, "the parser drops the trailing newline") ||
		!strings.Contains(detail, "handle EOF without a newline") {
		t.Fatalf("detail missing lens/rationale/feedback: %q", detail)
	}
	if !strings.Contains(detail, "fix the parser bug") || !strings.Contains(detail, "fixed the EOF handling") {
		t.Fatalf("detail should show the evidence the council saw (task/report): %q", detail)
	}
	m.councilDetail = nil

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
	// Outcome word is "reject" (termination), tally counts and feedback still shown.
	if !strings.Contains(last.text, "reject") || !strings.Contains(last.text, "1 done / 2 continue") || !strings.Contains(last.text, "feedback injected") {
		t.Fatalf("decided line missing reject/tally/feedback: %q", last.text)
	}

	// Turn end clears the council indicator (chip disappears).
	m.applyEvent(event.Event{Type: event.TypeTurnFinished})
	if m.councilRound != 0 || m.councilMember != "" {
		t.Fatalf("council indicator should clear on turn finish: round=%d member=%q", m.councilRound, m.councilMember)
	}
}

// A plan audit forced to finish at the round cap (all-revise, no consensus) must
// NOT read "approve" — it reads "proceed (no consensus)".
func TestCouncilPlanForcedProceedLabel(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Phase: "plan", Decision: "done",
		Tally: council.Breakdown{Continue: 3},
		Note:  "plan unresolved after 2 round(s) — proceeding",
	}))
	last := m.blocks[len(m.blocks)-1]
	if !strings.Contains(last.text, "proceed (no consensus)") {
		t.Fatalf("forced plan finish should read 'proceed (no consensus)', got %q", last.text)
	}
	if strings.Contains(last.text, ": approve ") {
		t.Fatalf("forced finish must not read 'approve': %q", last.text)
	}
	// Plan tally uses approve/revise wording, not done/continue.
	if !strings.Contains(last.text, "3 revise") || strings.Contains(last.text, "continue") {
		t.Fatalf("plan tally should read 'N revise', not 'continue': %q", last.text)
	}

	// Termination no-progress forced finish (note ends in "finishing", 0 done) must
	// read "finished (no consensus)", not "done".
	m2 := newTestModel(t)
	m2.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Decision: "done", Tally: council.Breakdown{Continue: 3},
		Note: "members voted continue but gave no new feedback — finishing",
	}))
	l2 := m2.blocks[len(m2.blocks)-1]
	if !strings.Contains(l2.text, "finished (no consensus)") || strings.Contains(l2.text, ": done ") {
		t.Fatalf("no-progress forced finish should read 'finished (no consensus)', got %q", l2.text)
	}
}
