package app

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// PlanChildren joins the parent link with the per-child ParentStep edge: it returns
// only the children spawned for the queried step, in creation order, and excludes
// children of other steps, of other parents, and non-plan spawns (nil ParentStep).
func TestPlanChildren(t *testing.T) {
	step := func(n int) *int { return &n }
	base := time.Now()
	a := &App{sessions: map[session.SessionID]session.Session{
		"parent":    {ID: "parent"},
		"c_late":    {ID: "c_late", Parent: "parent", ParentStep: step(0), Created: base.Add(2 * time.Second)},
		"c_early":   {ID: "c_early", Parent: "parent", ParentStep: step(0), Created: base},
		"c_step1":   {ID: "c_step1", Parent: "parent", ParentStep: step(1), Created: base},
		"c_council": {ID: "c_council", Parent: "parent", Created: base}, // nil ParentStep (non-plan spawn)
		"c_other":   {ID: "c_other", Parent: "elsewhere", ParentStep: step(0), Created: base},
	}}

	got := a.PlanChildren("parent", 0)
	want := []session.SessionID{"c_early", "c_late"} // creation order, step-0 children of parent only
	if len(got) != len(want) {
		t.Fatalf("step 0: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step 0 order: got %v, want %v", got, want)
		}
	}

	if got := a.PlanChildren("parent", 1); len(got) != 1 || got[0] != "c_step1" {
		t.Fatalf("step 1: got %v, want [c_step1]", got)
	}
	if got := a.PlanChildren("parent", 9); len(got) != 0 {
		t.Fatalf("step with no children should be empty, got %v", got)
	}
}
