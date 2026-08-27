package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// A council nobody could reach comes back as three abstentions, and the gate holds the turn open
// on it — correctly, since an unreachable council may not bless a finish. What the screen must not
// do is spell that "reject": the members never read the work, and the repair a reader would go for
// (fix the answer) is not the repair the run needs (fix the backend).
func TestUnansweredRoundIsNotARejection(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue),
		Tally: council.Breakdown{Abstain: 3, Silent: 3, Voters: 0},
	}))
	line := m.blocks[len(m.blocks)-1].text
	if strings.Contains(line, "reject") {
		t.Errorf("a round with no votes in it is not a rejection: %q", line)
	}
	if !strings.Contains(line, "no verdict") || !strings.Contains(line, "3 no answer") {
		t.Errorf("the line must say the council did not answer: %q", line)
	}
	if strings.Contains(line, "3 abstain") {
		t.Errorf("nobody abstained — they were never reached: %q", line)
	}
}

// The other half: a member that DID abstain still reads as an abstention, and a mixed round names
// both counts apart.
func TestAbstainAndNoAnswerAreCountedApart(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Decision: string(council.Done),
		Tally: council.Breakdown{Done: 2, Abstain: 2, Silent: 1, Voters: 2},
	}))
	line := m.blocks[len(m.blocks)-1].text
	if !strings.Contains(line, "1 abstain") || !strings.Contains(line, "1 no answer") {
		t.Errorf("one declined and one was never reached — both must show: %q", line)
	}
}

// A member's own row says which of the two it was. Both carry decision "abstain" on the wire
// (the tally must not count either), so the row cannot be read off the decision alone.
func TestSilentMemberRowSaysNoAnswer(t *testing.T) {
	silent := councilMemberPlainAt(event.CouncilVerdictData{
		Member: "balthasar", Decision: string(council.Abstain), Silent: true}, councilDetailNameOnly)
	if !strings.Contains(silent, "no answer") || strings.Contains(silent, "abstain") {
		t.Errorf("a member that was never reached did not abstain: %q", silent)
	}
	said := councilMemberPlainAt(event.CouncilVerdictData{
		Member: "casper", Decision: string(council.Abstain)}, councilDetailNameOnly)
	if !strings.Contains(said, "abstain") {
		t.Errorf("a member that chose to abstain still abstains: %q", said)
	}
}
