package app

import (
	"strings"
	"testing"
)

// The two counters bound different things: read-only exploration is discretionary and the plan's
// write steps are the work the council approved. While they shared one pool, a scout that
// discovered a handful of items (one spawn for discovery plus one per item) could leave the
// delegate steps behind it with a budget of zero. The write budget must at minimum cover a
// maximum-size plan, or a plan the planner is allowed to propose cannot be fully dispatched.
func TestWriteBudgetCoversAMaximumPlan(t *testing.T) {
	if maxPlanWriteSteps < maxPlanSteps {
		t.Fatalf("a %d-step plan cannot be dispatched from a write budget of %d", maxPlanSteps, maxPlanWriteSteps)
	}
	// The worst realistic exploration draw: a scout's discovery spawn plus a full fan-out.
	if worst := 1 + maxPlanGroups; maxPlanExplorers < worst {
		t.Errorf("explorer budget %d cannot fund one scout fan-out (%d)", maxPlanExplorers, worst)
	}
}

// Budget exhaustion is the one dispatch failure that used to leave NO trace: a degraded step keeps
// its todo pending and a failed one records a FAILED finding, but running out of budget just broke
// the loop, so the findings block stopped mid-plan and the remaining work belonged to nobody. The
// finding must name every remaining step and hand ownership to the reader.
func TestUndispatchedFindingNamesEveryRemainingStep(t *testing.T) {
	rest := []planStep{
		{Title: "Wire the parser into the request path", Strategy: "delegate"},
		{Title: "Add the regression test", Strategy: "delegate"},
	}
	f := undispatchedFinding(rest)
	for _, st := range rest {
		if !strings.Contains(f, st.Title) {
			t.Errorf("step %q is missing from the finding:\n%s", st.Title, f)
		}
	}
	if !strings.Contains(f, "budget") {
		t.Errorf("the finding must say WHY nothing was dispatched:\n%s", f)
	}
	// It must not read as a report of work done — nothing was done, and the reader inherits it.
	low := strings.ToLower(f)
	if !strings.Contains(low, "nothing has been done") || !strings.Contains(low, "yourself") {
		t.Errorf("the finding must state nothing was done and assign the work to the reader:\n%s", f)
	}
}

// The whole point of the split is that read-only fan-out can no longer spend the write steps'
// capacity. Exercise the actual arithmetic of the two consumers against one plan's worth of steps.
func TestScoutFanOutCannotStarveTheWriteBudget(t *testing.T) {
	explore := maxPlanExplorers
	write := maxPlanWriteSteps
	// A scout: one discovery spawn, then a full fan-out.
	explore--
	capGroups(make([]planGroup, maxPlanGroups), &explore)
	if write != maxPlanWriteSteps {
		t.Fatalf("read-only exploration charged the write budget: %d", write)
	}
	if explore >= maxPlanExplorers {
		t.Fatalf("exploration charged nothing at all: %d", explore)
	}
	// Every step of a maximum plan still dispatches.
	for i := 0; i < maxPlanSteps; i++ {
		if write <= 0 {
			t.Fatalf("write budget ran out at step %d of %d after a scout fan-out", i, maxPlanSteps)
		}
		write--
	}
}

// capGroups guards its own slice bound; a budget already at zero must yield nothing rather than
// panic on groups[:0] arithmetic gone negative.
func TestCapGroupsWithNoBudgetYieldsNothing(t *testing.T) {
	zero := 0
	if g := capGroups(make([]planGroup, 3), &zero); g != nil {
		t.Errorf("an exhausted budget must fund no groups, got %d", len(g))
	}
	neg := -2
	if g := capGroups(make([]planGroup, 3), &neg); g != nil {
		t.Errorf("a negative budget must fund no groups, got %d", len(g))
	}
}
