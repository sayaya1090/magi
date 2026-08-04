package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// The tally on a decided line is taken AFTER the rebuttal, so a round that started 2-1
// and ended 3-0 renders as unanimity that never happened. These assert the line says
// which of the two it was.
func TestDecidedLineNamesTheRebuttal(t *testing.T) {
	decide := func(d *council.DebateOutcome, tally council.Breakdown) string {
		mm := newTestModel(t)
		m := &mm
		m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
			Round: 1, Decision: string(council.Done), Tally: tally, Debate: d,
		}))
		return m.blocks[len(m.blocks)-1].text
	}
	unanimous := council.Breakdown{Done: 3}

	// No debate ran: the line must not claim one did.
	if got := decide(nil, unanimous); strings.Contains(got, "debated") {
		t.Errorf("a unanimous vote runs no rebuttal, so the line must not mention one: %q", got)
	}

	// The rebuttal turned the council around — the interesting case, and the one a bare
	// 3-0 hides completely.
	got := decide(&council.DebateOutcome{Before: council.Continue, After: council.Done, Changed: 2}, unanimous)
	if !strings.Contains(got, "continue→done") || !strings.Contains(got, "2 members moved") {
		t.Errorf("a flipped outcome must name before→after and the count: %q", got)
	}

	// Members moved but the outcome stood: still worth saying, and it is NOT a flip.
	got = decide(&council.DebateOutcome{Before: council.Done, After: council.Done, Changed: 1}, unanimous)
	if strings.Contains(got, "→") {
		t.Errorf("nothing flipped, so no arrow: %q", got)
	}
	if !strings.Contains(got, "done held") || !strings.Contains(got, "1 member moved") {
		t.Errorf("a held outcome must say so, singular: %q", got)
	}

	// Debate ran and moved nobody. Three members who heard each other and did not budge
	// is a stronger reading than three who never spoke — the line has to tell them apart.
	got = decide(&council.DebateOutcome{Before: council.Done, After: council.Done, Changed: 0}, unanimous)
	if !strings.Contains(got, "no one moved") {
		t.Errorf("a rebuttal that moved nobody must still be visible: %q", got)
	}
}

// The whole point is that the outcome survives the trip through the event log, not just
// that the struct has a field.
func TestDebateSurvivesTheEventPayload(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Done), Tally: council.Breakdown{Done: 2, Continue: 1},
		Debate: &council.DebateOutcome{Before: council.Continue, After: council.Done, Changed: 1},
	}))
	got := m.blocks[len(m.blocks)-1].text
	if !strings.Contains(got, "2 done / 1 continue") {
		t.Errorf("the tally still renders alongside: %q", got)
	}
	if !strings.Contains(got, "continue→done") {
		t.Errorf("debate lost in transit through the payload: %q", got)
	}
}

// An open round is redrawn, not reprinted.
//
// Reported after the verdicts started streaming: the second and third arrivals came out stacked on
// top of the first, each showing everything before it. The transcript is INLINE — bubbletea owns
// only the lines it has not scrolled past — so a committed row that grows leaves every earlier
// version of itself permanently on screen. One member, then two, then three.
//
// So the open round lives in the redrawn area and becomes exactly one block when it closes.
func TestAnOpenRoundIsNotCommittedUntilItCloses(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 120
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
	}))
	names := []string{"Melchior", "Balthasar", "Casper"}
	for i, n := range names {
		m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
			Round: 1, Member: n, Lens: "correctness", Decision: "done", Confidence: 0.9}))
		if got := len(m.liveVerdicts); got != i+1 {
			t.Fatalf("after %d votes the live round holds %d", i+1, got)
		}
		for _, b := range m.blocks {
			if b.kind == blockCouncilVerdict {
				t.Fatalf("vote %d committed a row while the round was still open", i+1)
			}
		}
		// Whatever has landed is visible — the point of streaming is that you can watch it.
		if !strings.Contains(ansi.Strip(m.councilRow(m.liveVerdicts)), n) {
			t.Errorf("%s landed but is not on the live row", n)
		}
	}

	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Done), Tally: council.Breakdown{Done: 3, Voters: 3},
	}))
	if len(m.liveVerdicts) != 0 {
		t.Errorf("the closed round is still live: %d", len(m.liveVerdicts))
	}
	rows, members := 0, 0
	for _, b := range m.blocks {
		if b.kind == blockCouncilVerdict {
			rows++
			members += len(b.councilVerdicts)
		}
	}
	if rows != 1 || members != 3 {
		t.Errorf("a closed round is ONE row of three, got %d rows / %d members", rows, members)
	}
}
