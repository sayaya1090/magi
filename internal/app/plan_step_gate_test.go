package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// verifyStepChecks is the deterministic half of the step gate: a delegate worker's "done" is accepted
// only when its step's deliverable checks actually pass. A failing check must reject the step and
// report WHY (so re-planning adapts); passing checks clear it; the flag off makes it inert.
func TestVerifyStepChecks(t *testing.T) {
	ctx := context.Background()

	t.Run("pass", func(t *testing.T) {
		t.Setenv("MAGI_STEP_VERIFY", "1")
		plat := &scriptPlatform{codes: []int{0}}
		a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
		setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "out.txt", Command: "run"}})
		if pass, fails := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0); !pass || fails != "" {
			t.Fatalf("passing check → (%v, %q); want (true, \"\")", pass, fails)
		}
	})

	t.Run("fail rejects and explains", func(t *testing.T) {
		t.Setenv("MAGI_STEP_VERIFY", "1")
		plat := &scriptPlatform{codes: []int{1}} // check command exits non-zero
		a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
		setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "out.txt", Command: "run"}})
		pass, fails := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0)
		if pass {
			t.Fatal("a failing check must NOT let the step pass (a false 'done' would advance)")
		}
		if !strings.Contains(fails, "out.txt") {
			t.Errorf("the failure ledger must name the unmet deliverable so re-plan can adapt, got %q", fails)
		}
	})

	t.Run("only gates on THIS step's checks (strict, no cross-step false failure)", func(t *testing.T) {
		t.Setenv("MAGI_STEP_VERIFY", "1")
		plat := &scriptPlatform{codes: []int{1}} // any check that runs would FAIL
		a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
		// A check that belongs to step 4 ("server on port 5328") must NOT gate step 0 ("install deps"):
		// it cannot pass until a later step, and gating step 0 on it would falsely re-plan.
		setChecks(a, sid, []council.DeliverableCheck{{Step: "4", Deliverable: "server on port 5328", Command: "probe"}})
		pass, _ := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0)
		if !pass {
			t.Fatal("step 0 must not be gated on a step-4 check (strict match); this was the false-reject bug")
		}
		if plat.calls != 0 {
			t.Errorf("no check belongs to step 0 → none should run, ran %d", plat.calls)
		}
	})

	t.Run("flag off is inert", func(t *testing.T) {
		t.Setenv("MAGI_STEP_VERIFY", "0")
		plat := &scriptPlatform{codes: []int{1}}
		a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
		setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "run"}})
		if pass, _ := a.verifyStepChecks(ctx, a.sessionInfo(ctx, sid), 0); !pass {
			t.Error("with the flag off the gate must not block (returns pass)")
		}
		if plat.calls != 0 {
			t.Errorf("flag off must run no check command, ran %d", plat.calls)
		}
	})
}
