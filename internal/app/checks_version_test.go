package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// checksVersion bumps every time storePlanChecks (re)writes the deliverable-check set, so the
// incremental recorder fires when a mid-run re-plan DERIVES new checks (from criteria/constraints)
// for work that may already be done — a change that bumps neither the mutation epoch nor
// execActivity, and so would otherwise wait for the terminal gate.
func TestChecksVersionBumpsOnStore(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "0") // skip the provider validation pass
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)

	if v := a.checksVersion(sid); v != 0 {
		t.Fatalf("starts at 0, got %d", v)
	}
	a.storePlanChecks(ctx, s, []council.DeliverableCheck{{Step: "1", Command: "true"}})
	if v := a.checksVersion(sid); v != 1 {
		t.Errorf("after the first store → 1, got %d", v)
	}
	// A re-plan re-storing a (different) check set bumps it again — the mid-run derive case.
	a.storePlanChecks(ctx, s, []council.DeliverableCheck{{Step: "1", Command: "true"}, {Step: "2", Command: "true"}})
	if v := a.checksVersion(sid); v != 2 {
		t.Errorf("after a re-plan store → 2, got %d", v)
	}
}
