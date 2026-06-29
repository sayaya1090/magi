package council

import "testing"

// Boundary cases for the tally rules that the main table doesn't cover. These pin the
// repo's most safety-critical pure logic at its edges.

// weighted:θ uses ">= θ" (a THRESHOLD), so an exact tie at θ=0.5 finishes — this is
// deliberately distinct from `majority`, where a count-tie does NOT finish (strict >).
// (Design decision: weighted is an explicit threshold; ≥θ is its natural reading.)
func TestTallyWeightedExactTie(t *testing.T) {
	tie := []Verdict{vw("a", Done, 1), vw("b", Continue, 1)} // DoneWeight=1, total=2, 1 >= 0.5*2
	if got, _ := Tally(tie, "weighted:0.5"); got != Done {
		t.Errorf("weighted:0.5 exact tie = %q, want done (>=θ threshold)", got)
	}
	// Contrast: the SAME tie under majority does NOT finish.
	if got, _ := Tally([]Verdict{v("a", Done), v("b", Continue)}, RuleMajority); got != Continue {
		t.Errorf("majority tie = %q, want continue (strict majority)", got)
	}
	// Just under the threshold continues.
	under := []Verdict{vw("a", Done, 1), vw("b", Continue, 2)} // share 1/3
	if got, _ := Tally(under, "weighted:0.5"); got != Continue {
		t.Errorf("weighted:0.5 share 1/3 = %q, want continue", got)
	}
}

// Abstains are excluded from the denominator, so a lone done + an abstainer satisfies
// unanimity (every VOTER said done).
func TestTallyUnanimousWithAbstain(t *testing.T) {
	if got, _ := Tally([]Verdict{v("a", Done), v("b", Abstain)}, RuleUnanimous); got != Done {
		t.Errorf("unanimous [done, abstain] = %q, want done (abstain not a voter)", got)
	}
	// All abstain → no voters → never finish.
	if got, _ := Tally([]Verdict{v("a", Abstain), v("b", Abstain)}, RuleUnanimous); got != Continue {
		t.Errorf("unanimous all-abstain = %q, want continue (no affirmation)", got)
	}
}

func TestTallyVetoEdges(t *testing.T) {
	// The veto member ABSTAINING does not veto; the rest is a plain majority.
	abst := []Verdict{v("Melchior", Done), v("Casper", Done), v("Balthasar", Abstain)}
	if got, _ := Tally(abst, "veto:Balthasar"); got != Done {
		t.Errorf("veto member abstaining = %q, want done (no veto, majority holds)", got)
	}
	// The veto member voting DONE is not a veto, but a finish still needs a majority.
	noMaj := []Verdict{v("Melchior", Continue), v("Casper", Continue), v("Balthasar", Done)}
	if got, _ := Tally(noMaj, "veto:Balthasar"); got != Continue {
		t.Errorf("veto member done but no majority = %q, want continue", got)
	}
	// An empty veto list degrades to plain majority.
	if got, _ := Tally([]Verdict{v("a", Done), v("b", Done), v("c", Continue)}, "veto:"); got != Done {
		t.Errorf("empty veto list = %q, want done (degrades to majority)", got)
	}
}
