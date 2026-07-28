package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The kv-store-grpc case: the COUNCIL keeps rejecting the finish across repeated edit cycles — a
// contradictory acceptance condition the agent cannot satisfy. The churn counter accrues on each
// council rejection that followed an edit, and lands UNVERIFIED at the cap so the external verifier
// judges the live deliverable instead of an external hard-kill tearing it down.
func TestTerminationGateCouncilChurnLands(t *testing.T) {
	// The VOTING gate: rounds, the round cap, the no-progress stop, the deadlock landing. The
	// termination council now ADVISES by default, so this pins the behaviour behind
	// MAGI_COUNCIL_ADVISORY=0 — which keeps both the incident history these cases encode and a
	// genuinely exercised rollback path.
	t.Setenv("MAGI_COUNCIL_ADVISORY", "0")
	t.Setenv("MAGI_EXEC_EVIDENCE", "0") // isolate the council path (no authored-but-unrun nudge in front)
	t.Setenv("MAGI_CHECK_CHURN_CAP", "2")

	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{0, 0, 0, 0}}
	// The council rejects every finish (never approves) — the contradictory-check stand-in. Distinct
	// feedback per round so the council's own no-progress finish (repeated-feedback) never fires: this
	// isolates the run-scoped churn cap as the thing that lands the turn.
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "still unmet A"},
		{Round: 2, Decision: council.Continue, Feedback: "still unmet B"},
		{Round: 3, Decision: council.Continue, Feedback: "still unmet C"},
	}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 5})

	guard := newRunGuard()
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: guard, depth: 0, maxSteps: 50}
	ts := &turnState{prevFinishCalls: -1}

	// Cycle 1: the agent edited (epoch 1), council rejects → council churn 1/2, keep looping.
	guard.mutated("server.py", "v1")
	act, loop := a.runTerminationGate(ctx, tc, 1, "task", "answer v1", nil, true, ts)
	if !loop || act != loopContinue {
		t.Fatalf("cycle 1 (council churn 1/2) should keep looping, got act=%v loop=%v reason=%q", act, loop, ts.unverifiedReason)
	}
	if guard.checkFailChurn != 1 {
		t.Fatalf("a council rejection after an edit must count as churn, got %d", guard.checkFailChurn)
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
