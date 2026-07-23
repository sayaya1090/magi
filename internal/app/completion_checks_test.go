package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

// CompletionChecks annotates each check with a display state for the plan panel: a check whose
// verify command has run green is CheckPassed (✓); one whose step is the in-progress todo is
// CheckActive (spinner); everything else is CheckPending (bullet). A passed check outranks active.
func TestCompletionChecksState(t *testing.T) {
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})

	checks := []council.DeliverableCheck{
		{Step: "1", Deliverable: "proto compiled", Command: "test -f kv_store_pb2.py"},
		{Step: "2", Deliverable: "server responds", Command: "probe"},
		{Step: "3", Deliverable: "cleanup done", Command: "test ! -f tmp"},
	}
	setChecks(a, sid, checks)
	// Step 2 is the one in progress; steps 1 and 3 are pending/other.
	a.SetTodos(sid, []session.Todo{
		{Content: "compile proto", Status: "completed"},
		{Content: "run server", Status: "in_progress"},
		{Content: "clean up", Status: "pending"},
	})
	// The step-1 check has run green; record that result.
	a.recordCheckResult(sid, checks[0], true)

	got := a.CompletionChecks(sid)
	if len(got) != 3 {
		t.Fatalf("want 3 checks, got %d", len(got))
	}
	if got[0].State != CheckPassed {
		t.Errorf("check[0] (verified green) must be CheckPassed, got %v", got[0].State)
	}
	if got[1].State != CheckActive {
		t.Errorf("check[1] (step 2 in progress) must be CheckActive, got %v", got[1].State)
	}
	if got[2].State != CheckPending {
		t.Errorf("check[2] (step 3, not started) must be CheckPending, got %v", got[2].State)
	}

	// A passed check that is ALSO its step's in-progress todo still renders as passed (✓ outranks
	// the spinner): mark step 2 passed while its todo stays in_progress.
	a.recordCheckResult(sid, checks[1], true)
	got = a.CompletionChecks(sid)
	if got[1].State != CheckPassed {
		t.Errorf("a passed check must outrank the active spinner, got %v", got[1].State)
	}

	// A later failing run reverts the ✓ (the glyph reflects the latest result, not a latch).
	a.recordCheckResult(sid, checks[0], false)
	got = a.CompletionChecks(sid)
	if got[0].State == CheckPassed {
		t.Errorf("a check that later fails must not stay CheckPassed, got %v", got[0].State)
	}
}
