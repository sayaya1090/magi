package app

import "testing"

// capGroups caps a fan-out to the remaining explorer budget and debits it. It must handle an exhausted
// budget without a negative-index panic on groups[:*budget].
func TestCapGroups(t *testing.T) {
	g4 := []planGroup{{Focus: "a"}, {Focus: "b"}, {Focus: "c"}, {Focus: "d"}}

	// Under budget: all kept, budget debited by that many.
	b := 10
	if got := capGroups(g4, &b); len(got) != 4 || b != 6 {
		t.Fatalf("under budget: got %d groups, budget %d; want 4, 6", len(got), b)
	}
	// Over budget: capped to the budget, budget hits 0.
	b = 2
	if got := capGroups(g4, &b); len(got) != 2 || b != 0 {
		t.Fatalf("over budget: got %d groups, budget %d; want 2, 0", len(got), b)
	}
	// Zero budget: nothing fits, budget unchanged, no panic.
	b = 0
	if got := capGroups(g4, &b); got != nil || b != 0 {
		t.Fatalf("zero budget: got %v, budget %d; want nil, 0", got, b)
	}
	// Negative budget (over-spent elsewhere): nothing fits, no negative-slice panic.
	b = -3
	if got := capGroups(g4, &b); got != nil || b != -3 {
		t.Fatalf("negative budget: got %v, budget %d; want nil, -3", got, b)
	}
}
