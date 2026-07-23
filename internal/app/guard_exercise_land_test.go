package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// handleStuckGuard lands the run gracefully UNVERIFIED once the agent's OWN build/test command has
// failed across exerciseChurnCap distinct edits without ever passing — the solo-path counterpart to
// the delegate path's verifyStepChecks gate. This fires even when NO stall/idle/repeat kind trips
// (monotonic-novel edit churn keeps stuck() empty), using only the agent's executed results.
func TestHandleStuckGuardExerciseChurnLands(t *testing.T) {
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "3")

	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}
	u := event.Usage{}

	// Two edits with the same build still failing: below the cap, and no stall/idle kind trips → no land.
	for i := 1; i <= 2; i++ {
		guard.mutated("user.cpp", sig(i))
		guard.noteExerciseResult("make test", true)
	}
	if stop, _ := a.handleStuckGuard(ctx, tc, "task", nil, u, ts); stop {
		t.Fatalf("below the cap (churn 2/3) must not land, exerciseChurnMax=%d", guard.exerciseChurnMax())
	}
	if ts.unverifiedReason != "" {
		t.Fatalf("below the cap must not set a reason, got %q", ts.unverifiedReason)
	}

	// The third edit with the same build STILL failing hits the cap → land UNVERIFIED, cleanly.
	guard.mutated("user.cpp", sig(3))
	guard.noteExerciseResult("make test", true)
	stop, clean := a.handleStuckGuard(ctx, tc, "task", nil, u, ts)
	if !stop || !clean {
		t.Fatalf("at the cap (churn 3/3) must land cleanly, got stop=%v clean=%v", stop, clean)
	}
	if !strings.Contains(ts.unverifiedReason, "converging") {
		t.Fatalf("landing must set the non-converging reason, got %q", ts.unverifiedReason)
	}
}

// A build that eventually PASSES clears its churn count, so a legitimately-hard task that iterates
// before converging never trips the observed-churn landing.
func TestHandleStuckGuardExerciseChurnResetsOnPass(t *testing.T) {
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "3")

	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}
	u := event.Usage{}

	// Climb to 2, then the build passes → count clears; two more failing edits only reach 2 again.
	for i := 1; i <= 2; i++ {
		guard.mutated("user.cpp", sig(i))
		guard.noteExerciseResult("make test", true)
	}
	guard.mutated("user.cpp", sig(3))
	guard.noteExerciseResult("make test", false) // converged once → clear
	for i := 4; i <= 5; i++ {
		guard.mutated("user.cpp", sig(i))
		guard.noteExerciseResult("make test", true)
	}
	if guard.exerciseChurnMax() != 2 {
		t.Fatalf("a pass must have reset the count, exerciseChurnMax=%d want 2", guard.exerciseChurnMax())
	}
	if stop, _ := a.handleStuckGuard(ctx, tc, "task", nil, u, ts); stop {
		t.Fatalf("a converged-then-re-failed build below the cap must not land")
	}
}
