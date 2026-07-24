package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// noteReplan is convergence-aware: a replan that follows real step progress (the completed-step
// high-water climbs) resets the sterile count and re-baselines, while replans that finish no new
// step accumulate. So a plan that keeps completing steps — even across many replans — never trips,
// and only a re-decomposition loop that completes nothing climbs to the cap.
func TestNoteReplanConvergence(t *testing.T) {
	g := newRunGuard()

	// Three replans, no step ever completed → the sterile count climbs each time.
	if n := g.noteReplan(0); n != 1 {
		t.Fatalf("first sterile replan should be 1, got %d", n)
	}
	if n := g.noteReplan(0); n != 2 {
		t.Fatalf("second sterile replan should be 2, got %d", n)
	}
	if n := g.noteReplan(0); n != 3 {
		t.Fatalf("third sterile replan should be 3, got %d", n)
	}

	// A step actually finishes before the next replan (high-water climbs 0→2) → reset.
	if n := g.noteReplan(2); n != 0 {
		t.Fatalf("a replan after real progress must reset to 0, got %d", n)
	}
	if g.sterileReplanMax() != 0 {
		t.Fatalf("sterileReplanMax should be 0 after reset, got %d", g.sterileReplanMax())
	}

	// Replans at the same high-water (no further step done) accumulate again from 0.
	if n := g.noteReplan(2); n != 1 {
		t.Fatalf("sterile replan at the same high-water should be 1, got %d", n)
	}
	// Even completing the tail (2→3) re-baselines: genuine progress is never punished.
	if n := g.noteReplan(3); n != 0 {
		t.Fatalf("finishing one more step must reset, got %d", n)
	}
}

// MAGI_REPLAN_CAP: unset uses the default; a positive integer overrides; 0/garbage disables.
func TestSterileReplanCapFlag(t *testing.T) {
	t.Setenv("MAGI_REPLAN_CAP", "")
	if got := sterileReplanCap(); got != defaultSterileReplanCap {
		t.Errorf("unset must use default %d, got %d", defaultSterileReplanCap, got)
	}
	if !sterileReplanLandEnabled() {
		t.Error("default must be enabled")
	}
	t.Setenv("MAGI_REPLAN_CAP", "7")
	if got := sterileReplanCap(); got != 7 {
		t.Errorf("override must be 7, got %d", got)
	}
	t.Setenv("MAGI_REPLAN_CAP", "0")
	if sterileReplanCap() != 0 || sterileReplanLandEnabled() {
		t.Error("0 must disable the landing")
	}
	t.Setenv("MAGI_REPLAN_CAP", "nonsense")
	if sterileReplanCap() != 0 {
		t.Error("garbage must disable the landing")
	}
}

// handleStuckGuard lands the run gracefully UNVERIFIED once agent-initiated replans have finished no
// new step across sterileReplanCap passes — the plan-structural counterpart to the exercise-churn
// land. This fires even when NO stall/idle/repeat kind trips (novel edits each replan keep stuck()
// empty), using only magi's own replan/completed-step signals.
func TestHandleStuckGuardSterileReplanLands(t *testing.T) {
	t.Setenv("MAGI_REPLAN_CAP", "3")

	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}
	u := event.Usage{}

	// Two sterile replans: below the cap, and no stall/idle kind trips → no land.
	guard.noteReplan(0)
	guard.noteReplan(0)
	if stop, _ := a.handleStuckGuard(ctx, tc, "task", nil, u, ts); stop {
		t.Fatalf("below the cap (2/3) must not land, sterileReplanMax=%d", guard.sterileReplanMax())
	}
	if ts.unverifiedReason != "" {
		t.Fatalf("below the cap must not set a reason, got %q", ts.unverifiedReason)
	}

	// The third sterile replan hits the cap → land UNVERIFIED, cleanly.
	guard.noteReplan(0)
	stop, clean := a.handleStuckGuard(ctx, tc, "task", nil, u, ts)
	if !stop || !clean {
		t.Fatalf("at the cap (3/3) must land cleanly, got stop=%v clean=%v", stop, clean)
	}
	if !strings.Contains(ts.unverifiedReason, "high-water") {
		t.Fatalf("landing must set the non-advancing reason, got %q", ts.unverifiedReason)
	}
}

// Real step progress between replans re-baselines the high-water, so a plan that keeps completing
// steps never reaches the sterile-replan landing however many times it replans.
func TestHandleStuckGuardSterileReplanResetsOnProgress(t *testing.T) {
	t.Setenv("MAGI_REPLAN_CAP", "3")

	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}
	u := event.Usage{}

	// Climb to 2, then a step completes (high-water 0→1) → reset; two more sterile replans reach 2.
	guard.noteReplan(0)
	guard.noteReplan(0)
	guard.noteReplan(1) // real progress → reset
	guard.noteReplan(1)
	guard.noteReplan(1)
	if guard.sterileReplanMax() != 2 {
		t.Fatalf("progress must have reset the count, sterileReplanMax=%d want 2", guard.sterileReplanMax())
	}
	if stop, _ := a.handleStuckGuard(ctx, tc, "task", nil, u, ts); stop {
		t.Fatal("a progressing-then-replanning run below the cap must not land")
	}
}
