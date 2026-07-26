package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// A check that cannot be EVALUATED (126 — here an assertion with no `source` for it to be about) is a
// broken check, not a failed deliverable — so the step gate must NOT fail the step on it (which would
// churn the work). The worker's equivalent substitution and the council settle the goal instead.
func TestVerifyStepChecksUnexecutableDefers(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{0}} // never reached: the check names nothing to read
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "server on port", Assert: "matches LISTEN"}})

	pass, fails := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0)
	if !pass {
		t.Fatalf("an unevaluable check must NOT fail the step, got fails=%q", fails)
	}
	if plat.calls != 0 {
		t.Errorf("a check with no source must not reach the platform, calls=%d", plat.calls)
	}
}

// A genuinely-run check that fails (non-127) still gates the step — only the unexecutable case defers.
func TestVerifyStepChecksRealFailStillGates(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{1}} // the source is not readable → the deliverable failed
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "out.txt", Source: "out.txt", Assert: "nonempty"}})

	pass, _ := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0)
	if pass {
		t.Fatal("a check that RAN and failed (exit 1) must still gate the step")
	}
}

// The report renders a CHECK-SUBSTITUTION section so the equivalent-command evidence reaches the council.
func TestReportRendersSubstitution(t *testing.T) {
	r := &subReport{status: "done", substitutions: "check `ss -tlnp` unrunnable (ss absent); ran `python3 -c socket.connect(5328)` → connected → PASS"}
	out := r.result("built the server")
	if !strings.Contains(out, "CHECK-SUBSTITUTION:") || !strings.Contains(out, "socket.connect") {
		t.Fatalf("report must render the substitution evidence:\n%s", out)
	}
}
