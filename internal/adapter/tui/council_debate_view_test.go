package tui

import (
	"strings"
	"testing"

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
