package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// When a deliverable self-check keeps FAILING across repeated edit cycles (mutation epoch
// advancing, same check still failing), runTerminationGate lands the turn gracefully UNVERIFIED —
// leaving work standing for the external verifier — instead of looping forever. A converging
// check (all pass) resets the churn count so this never fires on a task whose check eventually passes.
func TestTerminationGateChurnLands(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_CHECK_CHURN_CAP", "2") // land on the 2nd edited-yet-still-failing cycle

	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{1, 1, 1, 1}} // the deliverable check fails every run
	fc := &fakeCouncil{}                              // must be non-nil for the gate to apply; never reached (we land first)
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "server", Command: "probe"}})

	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}

	// Cycle 1: the agent edited the deliverable (epoch 1), check still fails → keep looping.
	guard.mutated("server.py", "v1")
	act, loop := a.runTerminationGate(ctx, tc, 1, "task", "answer", nil, true, ts)
	if !loop || act != loopContinue {
		t.Fatalf("cycle 1 (churn 1/2) should keep looping, got act=%v loop=%v", act, loop)
	}
	if ts.unverifiedReason != "" {
		t.Fatalf("cycle 1 must not land yet, reason=%q", ts.unverifiedReason)
	}

	// Cycle 2: another edit (epoch 2), same check still fails → churn hits the cap → land UNVERIFIED.
	guard.mutated("server.py", "v2")
	act, loop = a.runTerminationGate(ctx, tc, 2, "task", "answer", nil, true, ts)
	if loop {
		t.Fatalf("cycle 2 (churn 2/2) must land (loop=false), got act=%v loop=%v", act, loop)
	}
	if !strings.Contains(ts.unverifiedReason, "converging") {
		t.Fatalf("landing must set the non-converging reason, got %q", ts.unverifiedReason)
	}
}

// A check that PASSES resets the churn counter, so a legitimately-hard task that iterates before
// converging never trips the graceful landing. Here cap=3, so a fail (churn 1) followed by a pass
// (reset to 0) stays well under the cap and does not land.
func TestTerminationGateChurnResetsOnPass(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_CHECK_CHURN_CAP", "3")

	ctx := context.Background()
	// fail, then PASS: the pass must clear the churn accrued by the fail.
	plat := &scriptPlatform{codes: []int{1, 0}}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done}}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "server", Command: "probe"}})

	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{}

	guard.mutated("server.py", "v1")
	a.runTerminationGate(ctx, tc, 1, "task", "answer", nil, true, ts) // fail → churn 1
	if guard.checkFailChurn != 1 {
		t.Fatalf("after one failing cycle, churn = %d, want 1", guard.checkFailChurn)
	}
	// The check now passes AND the council approves → resetCheckChurn clears the count; converged.
	guard.mutated("server.py", "v2")
	a.runTerminationGate(ctx, tc, 2, "task", "answer2", nil, true, ts)
	if guard.checkFailChurn != 0 {
		t.Fatalf("a converging (approved) cycle must reset the churn count, got %d", guard.checkFailChurn)
	}
}

// The kv-store-grpc case: the per-step deliverable checks PASS (the files exist), but the COUNCIL
// keeps rejecting the finish across repeated edit cycles — a contradictory acceptance condition the
// agent cannot satisfy. The run-scoped churn counter must NOT be zeroed just because the step gate
// passed; it accrues on each council rejection that followed an edit, and lands UNVERIFIED at the cap
// so the external verifier judges the live deliverable instead of an external hard-kill tearing it down.
func TestTerminationGateCouncilChurnLands(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_EXEC_EVIDENCE", "0") // isolate the council path (no authored-but-unrun nudge in front)
	t.Setenv("MAGI_CHECK_CHURN_CAP", "2")

	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{0, 0, 0, 0}} // per-step checks PASS every run (step gate clean)
	// The council rejects every finish (never approves) — the contradictory-check stand-in. Distinct
	// feedback per round so the council's own no-progress finish (repeated-feedback) never fires: this
	// isolates the run-scoped churn cap as the thing that lands the turn.
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "still unmet A"},
		{Round: 2, Decision: council.Continue, Feedback: "still unmet B"},
		{Round: 3, Decision: council.Continue, Feedback: "still unmet C"},
	}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 5})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "server", Command: "probe"}})

	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{prevFinishCalls: -1}

	// Cycle 1: step check passes, agent edited (epoch 1), council rejects → council churn 1/2, keep looping.
	guard.mutated("server.py", "v1")
	act, loop := a.runTerminationGate(ctx, tc, 1, "task", "answer v1", nil, true, ts)
	if !loop || act != loopContinue {
		t.Fatalf("cycle 1 (council churn 1/2) should keep looping, got act=%v loop=%v reason=%q", act, loop, ts.unverifiedReason)
	}
	if guard.checkFailChurn != 1 {
		t.Fatalf("a council rejection after an edit must count as churn even with the step gate clean, got %d", guard.checkFailChurn)
	}

	// Cycle 2: another edit (epoch 2), council still rejects → churn hits the cap → land UNVERIFIED.
	guard.mutated("server.py", "v2")
	act, loop = a.runTerminationGate(ctx, tc, 2, "task", "answer v2", nil, true, ts)
	if loop {
		t.Fatalf("cycle 2 (council churn 2/2) must land (loop=false), got act=%v", act)
	}
	if !strings.Contains(ts.unverifiedReason, "council kept rejecting") {
		t.Fatalf("landing must set the council-path non-converging reason, got %q", ts.unverifiedReason)
	}
}
