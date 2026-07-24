package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// applyCheckSubs rewrites the stored deliverable check to the working equivalent (matched by the
// original command) and records it ✓, so every later gate runs the command that actually works here.
func TestApplyCheckSubsRewritesCheck(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "server on port", Command: "ss -tlnp"}, // the broken original
	})

	a.applyCheckSubs(ctx, sid, []port.CheckSub{
		{Step: "1", Original: "ss -tlnp", Command: "python3 -c connect", Expect: "ok", Reason: "ss absent"},
	})

	checks := a.cachedChecks(sid)
	if len(checks) != 1 || checks[0].Command != "python3 -c connect" || checks[0].Expect != "ok" {
		t.Fatalf("check not rewritten to the equivalent: %+v", checks)
	}
	// And recorded ✓ (trusted — the review approved it, not re-run here).
	cs := a.CompletionChecks(sid)
	if len(cs) != 1 || cs[0].State != CheckPassed {
		t.Fatalf("rewritten check should be recorded ✓, got %+v", cs)
	}
}

// An unmatched substitution (no check with that original) is appended as a new check for its step.
func TestApplyCheckSubsAppendsWhenUnmatched(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "orig"}})
	a.applyCheckSubs(ctx, sid, []port.CheckSub{{Step: "2", Original: "nope", Command: "equiv"}})
	checks := a.cachedChecks(sid)
	if len(checks) != 2 || checks[1].Step != "2" || checks[1].Command != "equiv" {
		t.Fatalf("unmatched sub should append a new check: %+v", checks)
	}
}

// A solo agent's approved substitution rewrites its OWN session's checks (depth 0), and the pending
// queue is cleared so it is not re-reviewed.
func TestReviewSubstitutionsApproveSolo(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{127}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "ss -tlnp"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "ss -tlnp", Command: "python3 probe", Reason: "ss absent"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds := 0
	act, looped := a.reviewSubstitutions(ctx, tc, &rounds)
	if looped {
		t.Fatalf("an approved substitution must not loop, got act=%v", act)
	}
	if got := a.cachedChecks(sid); got[0].Command != "python3 probe" {
		t.Fatalf("approved solo substitution should rewrite the check, got %q", got[0].Command)
	}
	if len(a.pendingSubsOf(sid)) != 0 {
		t.Fatal("pending substitutions should be cleared after approval")
	}
}

// A critical review concern loops the agent to correct (loopContinue) and keeps the pending
// substitution so the corrected re-declaration replaces it; the check is NOT rewritten yet.
func TestReviewSubstitutionsCorrectionLoops(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Decision: council.Continue,
		Verdicts: []council.Verdict{{Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "weak proxy — exercise the RPC"}},
	}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{127}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "ss -tlnp"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "ss -tlnp", Command: "test -f x"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds := 0
	act, looped := a.reviewSubstitutions(ctx, tc, &rounds)
	if !looped || act != loopContinue {
		t.Fatalf("a critical concern must loop the agent, got act=%v looped=%v", act, looped)
	}
	if rounds != 1 {
		t.Fatalf("a correction round should be counted, rounds=%d", rounds)
	}
	if got := a.cachedChecks(sid); got[0].Command != "ss -tlnp" {
		t.Fatalf("a rejected substitution must NOT rewrite the check yet, got %q", got[0].Command)
	}
	if len(a.pendingSubsOf(sid)) != 1 {
		t.Fatal("pending substitution should be kept for the corrected re-declaration")
	}
}

// A delegated worker's approved substitution is stashed for the parent (not applied locally, since the
// worker session has no stored checks), and surfaces via takeApprovedSubs.
func TestReviewSubstitutionsApproveWorkerStashes(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{127}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "ss -tlnp", Command: "python3 probe"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 1, maxSteps: 50} // depth>0 = worker
	rounds := 0
	if _, looped := a.reviewSubstitutions(ctx, tc, &rounds); looped {
		t.Fatal("approved worker substitution must not loop")
	}
	got := a.takeApprovedSubs(sid)
	if len(got) != 1 || got[0].Command != "python3 probe" {
		t.Fatalf("approved worker substitution should be stashed for the parent, got %+v", got)
	}
}

// Necessity guard: when the ORIGINAL check command actually RUNS and PASSES here, the substitution is
// unneeded — it is dropped without convening the council, and the working original is kept unchanged.
func TestReviewSubstitutionsDropsUnneeded(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{0}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "ss -tlnp"}}) // original RUNS (exit 0) here
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "ss -tlnp", Command: "python3 probe", Reason: "works correctly"})

	s := a.sessionInfo(ctx, sid)
	if act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int)); looped {
		t.Fatalf("an unneeded substitution must not loop, got act=%v", act)
	}
	if fc.calls != 0 {
		t.Fatalf("an unneeded substitution must NOT convene the review council, calls=%d", fc.calls)
	}
	if got := a.cachedChecks(sid); got[0].Command != "ss -tlnp" {
		t.Fatalf("the working original must be kept, not rewritten, got %q", got[0].Command)
	}
	if len(a.pendingSubsOf(sid)) != 0 {
		t.Fatal("the unneeded substitution should be dropped")
	}
}

// Necessity guard: when the ORIGINAL check command RUNS and FAILS, that is a real deliverable failure,
// not a broken check — the substitution is refused (not applied, council not convened) and the agent is
// looped to reconsider (fix the deliverable) instead of substituting the failure away.
func TestReviewSubstitutionsRefusesRanFailed(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{1}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "test -s out"}}) // original RUNS and FAILS (exit 1)
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "test -s out", Command: "true", Reason: "dodging"})

	s := a.sessionInfo(ctx, sid)
	act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int))
	if !looped || act != loopContinue {
		t.Fatalf("a ran-and-failed original must loop the agent to reconsider, got act=%v looped=%v", act, looped)
	}
	if fc.calls != 0 {
		t.Fatalf("a real failure must NOT convene the substitution council, calls=%d", fc.calls)
	}
	if got := a.cachedChecks(sid); got[0].Command != "test -s out" {
		t.Fatalf("a failing original must NOT be substituted away, got %q", got[0].Command)
	}
}

// A mixed report — one substitution whose original is genuinely unrunnable (justified) and one whose
// original ran and FAILED (must be refused) — loops the agent for the failure but KEEPS the justified
// substitution pending, so it is not lost.
func TestReviewSubstitutionsMixedKeepsJustified(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	// Original for step 1 → exit 127 (unrunnable, justified); original for step 2 → exit 1 (ran & failed).
	plat := &scriptPlatform{codes: []int{127, 1}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Command: "ss -tlnp"},
		{Step: "2", Command: "test -s out"},
	})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "ss -tlnp", Command: "python3 probe"})
	a.addPendingSub(sid, port.CheckSub{Step: "2", Original: "test -s out", Command: "true"})

	s := a.sessionInfo(ctx, sid)
	act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int))
	if !looped || act != loopContinue {
		t.Fatalf("the ran-and-failed sub must loop the agent, got act=%v looped=%v", act, looped)
	}
	pend := a.pendingSubsOf(sid)
	if len(pend) != 1 || pend[0].Step != "1" {
		t.Fatalf("the justified (step 1) sub must be KEPT while the failure-masking (step 2) is dropped, got %+v", pend)
	}
}

func TestCheckCommandUnrunnable(t *testing.T) {
	cases := []struct {
		out  string
		code int
		want bool
	}{
		{"", 127, true}, // command not found
		{"", 126, true}, // found but not executable
		{"/bin/sh: 1: port_owner: not found", 1, true}, // sh not-found under a non-127 code
		{"bash: foo: command not found", 2, true},
		{"verify output", 0, false},                      // ran and passed
		{"assertion failed", 1, false},                   // ran and failed — NOT unrunnable
		{"cat: /x: No such file or directory", 1, false}, // ran, failed on its ARGUMENT — not unrunnable
		{"key: not found\n", 0, false},                   // ran SUCCESSFULLY, just printed "not found" — runnable (exit-0 guard)
	}
	for _, c := range cases {
		if got := checkCommandUnrunnable(c.out, c.code); got != c.want {
			t.Errorf("checkCommandUnrunnable(%q, %d) = %v, want %v", c.out, c.code, got, c.want)
		}
	}
}
