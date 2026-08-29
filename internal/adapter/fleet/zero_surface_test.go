package fleet

import (
	"strings"
	"testing"
)

// Roster is for the message when an address matched nobody: who there IS, or the honest empty.
func TestRosterNamesWhoIsHere(t *testing.T) {
	if got := Roster(nil); !strings.Contains(got, "nobody") {
		t.Fatalf("an empty machine says so, got %q", got)
	}
	got := Roster([]Agent{
		{Name: "zeta"},
		{Name: "api", Role: "serves the API"},
	})
	if !strings.Contains(got, "api (serves the API)") || !strings.Contains(got, "zeta") {
		t.Fatalf("names with their roles, got %q", got)
	}
	if strings.Index(got, "api") > strings.Index(got, "zeta") {
		t.Fatalf("sorted, so the same fleet always reads the same: %q", got)
	}
}

// WordFrom labels a mid-exchange message: it must carry the mark the no-chaining rule reads, name
// the sender, and say how to answer — and it must NOT be the dispatch mark, which would freeze the
// receiver out of handing anything on.
func TestWordFromCarriesTheMarkAndTheWayBack(t *testing.T) {
	got := WordFrom("design")
	if !strings.HasPrefix(got, WordMark+"design") {
		t.Fatalf("the mark opens it, got %q", got)
	}
	if !strings.Contains(got, "mcp__design__ask") {
		t.Fatalf("the way to answer rides the label, got %q", got)
	}
}
