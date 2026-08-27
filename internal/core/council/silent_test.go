package council

import "testing"

// Silent verdicts are abstentions for the arithmetic (they must never count as votes) and a fact
// of their own for the reader. The tally has to carry both, or a surface cannot tell "declined"
// from "never answered" without re-reading every verdict.
func TestTallyCountsSilentInsideAbstain(t *testing.T) {
	b := tallyVotes([]Verdict{
		{Member: "melchior", Decision: Done},
		{Member: "balthasar", Decision: Abstain},            // chose not to vote
		{Member: "casper", Decision: Abstain, Silent: true}, // never reached
	})
	if b.Abstain != 2 || b.Silent != 1 {
		t.Fatalf("abstain=%d silent=%d — silent is a subset of abstain", b.Abstain, b.Silent)
	}
	if b.Voters != 1 {
		t.Fatalf("voters=%d — neither abstention is a vote", b.Voters)
	}
}

// A round nobody answered may not finish the turn: an unreachable council has not approved
// anything. The outcome is Continue, and the surfaces say WHY it is not a rejection (Breakdown.Silent).
func TestRoundWithNoVotesDoesNotFinish(t *testing.T) {
	vs := []Verdict{
		{Member: "melchior", Decision: Abstain, Silent: true},
		{Member: "balthasar", Decision: Abstain, Silent: true},
		{Member: "casper", Decision: Abstain, Silent: true},
	}
	d, b := Tally(vs, DefaultRule)
	if d != Continue {
		t.Fatalf("decision=%v — a council that never answered cannot bless a finish", d)
	}
	if b.Voters != 0 || b.Silent != 3 {
		t.Fatalf("voters=%d silent=%d", b.Voters, b.Silent)
	}
}
