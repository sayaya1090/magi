package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// A re-ask is asked for the steps still missing, so its answer is a PART of the fill and not a
// replacement for it. Merging every attempt against the ORIGINAL authored set made two
// complementary answers into alternatives, and the wider-wins comparison then discarded the one
// that arrived second.
//
// Observed live on a 6-step plan with 2 steps gated: the fill returned checks for steps 4, 5 and 6;
// the re-ask was told steps 1 and 3 were missing and returned exactly those; and the run landed
// with "step(s) 1, 3 still have NO check and land unverified". Their union covers all six.
func TestCoverageAccumulatesAcrossAttempts(t *testing.T) {
	steps := []planStep{{Title: "s1"}, {Title: "s2"}, {Title: "s3"},
		{Title: "s4"}, {Title: "s5"}, {Title: "s6"}}
	authored := []council.DeliverableCheck{
		{Step: "2", Deliverable: "d2", Source: "a.log", Assert: "nonempty"},
		{Step: "6", Deliverable: "d6", Source: "b.log", Assert: "nonempty"},
	}
	first := []council.DeliverableCheck{
		{Step: "4", Deliverable: "d4", Source: "c.log", Assert: "nonempty"},
		{Step: "5", Deliverable: "d5", Source: "d.log", Assert: "nonempty"},
	}
	second := []council.DeliverableCheck{
		{Step: "1", Deliverable: "d1", Source: "e.log", Assert: "nonempty"},
		{Step: "3", Deliverable: "d3", Source: "f.log", Assert: "nonempty"},
	}
	covered := func(cs []council.DeliverableCheck) map[int]bool {
		m := map[int]bool{}
		for _, c := range cs {
			if n := leadingInt(c.Step); n >= 1 && n <= len(steps) {
				m[n] = true
			}
		}
		return m
	}

	// What the run used to do: each attempt merged against the authored set.
	a1, _ := unionChecks(first, authored)
	a2, _ := unionChecks(second, authored)
	if len(covered(a1)) != 4 || len(covered(a2)) != 4 {
		t.Fatalf("precondition: each attempt alone covers 4, got %d and %d", len(covered(a1)), len(covered(a2)))
	}
	if len(covered(a2)) > len(covered(a1)) {
		t.Fatal("precondition: the second is not WIDER, which is why wider-wins dropped it")
	}

	// What it does now: the second merges into the first's result.
	cum, _ := unionChecks(second, a1)
	if got := len(covered(cum)); got != len(steps) {
		t.Fatalf("the two complementary answers must cover every step, got %d/%d", got, len(steps))
	}
	// …and nothing authored is lost on the way — the merge only ever adds.
	for _, c := range authored {
		var found bool
		for _, m := range cum {
			if m.Source == c.Source {
				found = true
			}
		}
		if !found {
			t.Errorf("authored check %q was lost in the accumulation", c.Source)
		}
	}
}
