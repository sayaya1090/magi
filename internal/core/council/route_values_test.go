package council

import (
	"strings"
	"testing"
)

// Whether a reported number is one its subject admits needs no cross-walk view: one reader with the
// value and the domain can settle it. It lived in the round's closing call, which is the one place
// that view is NOT needed, and the cost showed up measured — three lenses marked a set of fitted
// values SATISFIED three rounds running because a real command on the real input had produced them,
// while every one of those values was off by the same factor from what the subject allows.
//
// So it belongs to a lens, and to this one: correctness already walks the literal values and the
// premises, and "is this number possible" is the same walk one step further.
func TestCorrectnessWalksTheValuesThemselves(t *testing.T) {
	r := RouteFor("correctness")
	for _, want := range []string{
		"SUBJECT admits", // the question itself
		"CONSISTENCY",    // values agreeing because they share one bad input
		"SAME factor",    // the signature of a single upstream cause
		"AGENT'S OWN EXPLANATION",
		"TOOL RETURNED", // the only thing that resolves either trap
	} {
		if !strings.Contains(r, want) {
			t.Fatalf("the correctness route no longer asks about the values themselves (missing %q):\n%s", want, r)
		}
	}
	// It must stay distinguishable from provenance, which is the mark it was being confused with.
	if !strings.Contains(r, "not the same question") {
		t.Fatalf("the route must separate 'is this value possible' from 'where did it come from':\n%s", r)
	}
}

// The routes are ORDER OF SEARCH, never jurisdiction: a defect one member walks past must still be
// reachable by the other two, or a majority of the uninformed carries it.
func TestRoutesDoNotFenceOffJurisdiction(t *testing.T) {
	for lens, r := range Routes {
		for _, forbidden := range []string{"only you", "not your concern", "leave that to", "do not judge"} {
			if strings.Contains(strings.ToLower(r), forbidden) {
				t.Fatalf("%s route fences off jurisdiction (%q)", lens, forbidden)
			}
		}
	}
}
