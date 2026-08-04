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

// Each member is printed once, when it answers.
//
// Reported after the verdicts started streaming: the second and third arrivals came out stacked on
// the first, each showing everything before it. The transcript is INLINE — bubbletea repaints only
// the lines it still owns, and anything scrolled above is permanent — so a row that grew as members
// landed left every earlier version of itself on screen. Streaming turned a redraw that used to
// finish inside one frame into three separated by a minute.
//
// So nothing is redrawn: one line per member, as it lands.
func TestEachMemberIsPrintedOnceAsItAnswers(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 120
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority"}))
	rows := func() []string {
		var out []string
		for _, b := range m.blocks {
			if b.kind == blockCouncilVerdict {
				out = append(out, ansi.Strip(m.renderBlock(b)))
			}
		}
		return out
	}
	vote := func(who, dec string) event.Event {
		return ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
			Round: 1, Member: who, Lens: "correctness", Decision: dec, Confidence: 0.9})
	}
	names := []string{"Melchior", "Balthasar", "Casper"}
	for i, n := range names {
		m.applyEvent(vote(n, "done"))
		r := rows()
		if len(r) != i+1 {
			t.Fatalf("after %d votes there are %d rows — a member is redrawn or missing", i+1, len(r))
		}
		// The newest row is that member ALONE: a row carrying the earlier members too is the
		// stacked reprint this replaces.
		last := r[len(r)-1]
		if !strings.Contains(last, n) {
			t.Errorf("the new row is not %s: %q", n, last)
		}
		for _, other := range names[:i] {
			if strings.Contains(last, other) {
				t.Errorf("%s's row also carries %s — that is the accumulating reprint: %q", n, other, last)
			}
		}
	}

	// The recorded facts arrive after the previews. Identical ones are the same news and must not
	// print again.
	for _, n := range names {
		m.applyEvent(vote(n, "done"))
	}
	if got := len(rows()); got != 3 {
		t.Errorf("the facts reprinted the round: %d rows", got)
	}

	// A fact that DIFFERS is a rebuttal changing that member's mind — news of its own.
	m.applyEvent(vote("Balthasar", "continue"))
	r := rows()
	if len(r) != 4 {
		t.Fatalf("a changed vote must be shown, got %d rows", len(r))
	}
	if !strings.Contains(r[3], "Balthasar") || !strings.Contains(r[3], "reject") {
		t.Errorf("the revision is not the new row: %q", r[3])
	}

	// The round closes; the next one starts with nothing printed.
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Done: 2, Continue: 1}}))
	if m.drawnVerdicts != nil {
		t.Errorf("a closed round still remembers what it printed: %v", m.drawnVerdicts)
	}
}
