package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
)

// The console reads the same record the terminal does, and neither may spell an unreachable
// council as a rejection: "reject" says the members read the work and turned it down.
func TestConsoleSaysTheCouncilDidNotAnswer(t *testing.T) {
	out := councilText(app.CouncilMark{Round: 1, Decision: "continue", Silent: true,
		Tally: "0 done, 0 continue of 0 (3 no answer)"})
	if strings.Contains(out, "reject") {
		t.Errorf("no votes were cast, so nothing was rejected: %q", out)
	}
	if !strings.Contains(out, "did not answer") || !strings.Contains(out, "3 no answer") {
		t.Errorf("the row must say the council never answered: %q", out)
	}
	// A round that DID vote still reads as a rejection — that word is right there.
	voted := councilText(app.CouncilMark{Round: 1, Decision: "continue",
		Tally: "0 done, 3 continue of 3"})
	if !strings.Contains(voted, "reject") {
		t.Errorf("three members voted to continue — that is a rejection: %q", voted)
	}
}

// One member's row, same distinction.
func TestConsoleMemberRowSaysNoAnswer(t *testing.T) {
	silent := councilText(app.CouncilMark{Round: 1, Member: "casper", Decision: "abstain",
		Silent: true, Why: "council member unavailable: dial tcp: connection refused"})
	if !strings.Contains(silent, "no answer") || strings.Contains(silent, "∅ abstain") {
		t.Errorf("a member that was never reached did not abstain: %q", silent)
	}
	said := councilText(app.CouncilMark{Round: 1, Member: "balthasar", Decision: "abstain",
		Why: "outside my lens"})
	if !strings.Contains(said, "abstain") {
		t.Errorf("a member that chose to abstain still abstains: %q", said)
	}
}
