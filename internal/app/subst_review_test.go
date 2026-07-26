package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// applyCheckSubs rewrites the stored deliverable check to the working equivalent (matched by the
// original identity) and records it ✓, so every later gate reads the source that actually exists.
func TestApplyCheckSubsRewritesCheck(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	ctx := context.Background()
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{
		// The broken original: it names no source, so the runner can evaluate nothing.
		{Step: "1", Deliverable: "server on port", Assert: "matches listening"},
	})

	a.applyCheckSubs(ctx, sid, []port.CheckSub{
		{Step: "1", Original: "matches listening", Source: "server.log", Assert: "matches ^listening on",
			Reason: "the check named no source; the step records server.log"},
	})

	checks := a.cachedChecks(sid)
	if len(checks) != 1 || checks[0].Source != "server.log" || checks[0].Assert != "matches ^listening on" {
		t.Fatalf("check not rewritten to the equivalent: %+v", checks)
	}
	// And recorded ✓ (trusted — the review approved it, not re-run here).
	cs := a.CompletionChecks(sid)
	if len(cs) != 1 || cs[0].State != CheckPassed {
		t.Fatalf("rewritten check should be recorded ✓, got %+v", cs)
	}
}

// When a step has SEVERAL checks, applyCheckSubs rewrites only the one whose identity matches the sub's
// Original — the other checks for that step are left untouched (no clobber, no accidental append).
func TestApplyCheckSubsMultipleChecksPerStep(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Source: "wrong.log", Assert: "nonempty"}, // the broken one, to be replaced
		{Step: "1", Source: "pid", Assert: "process_alive"},  // a sibling check on the same step
	})
	a.applyCheckSubs(ctx, sid, []port.CheckSub{
		{Step: "1", Original: checkIdent(council.DeliverableCheck{Source: "wrong.log", Assert: "nonempty"}),
			Source: "server.log", Assert: "nonempty"},
	})
	checks := a.cachedChecks(sid)
	if len(checks) != 2 {
		t.Fatalf("must rewrite in place, not append: %+v", checks)
	}
	if checks[0].Source != "server.log" {
		t.Fatalf("the matching check must be rewritten, got %q", checks[0].Source)
	}
	if checks[1].Assert != "process_alive" || checks[1].Source != "pid" {
		t.Fatalf("the sibling check must be untouched, got %+v", checks[1])
	}
}

// An unmatched substitution (no check on that step) is appended as a new check for its step.
func TestApplyCheckSubsAppendsWhenUnmatched(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "a.log", Assert: "nonempty"}})
	a.applyCheckSubs(ctx, sid, []port.CheckSub{{Step: "2", Original: "nope", Source: "b.log", Assert: "nonempty"}})
	checks := a.cachedChecks(sid)
	if len(checks) != 2 || checks[1].Step != "2" || checks[1].Source != "b.log" {
		t.Fatalf("unmatched sub should append a new check: %+v", checks)
	}
}

// When a step has exactly ONE check and the sub's Original does not match it (or is empty), applyCheckSubs
// falls back to that sole check and rewrites it in place — it does NOT append a duplicate. This locks the
// `len(stepIdxs) == 1` fallback branch (distinct from the exact-match and the no-step-match/append paths).
func TestApplyCheckSubsSoleCheckFallback(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "orig.log", Assert: "nonempty"}})
	// Original is blank (the worker did not echo the exact original), but the step has a single check,
	// so the sub still lands on it rather than appending a second check for the same step.
	a.applyCheckSubs(ctx, sid, []port.CheckSub{{Step: "1", Source: "real.log", Assert: "matches ^OK"}})
	checks := a.cachedChecks(sid)
	if len(checks) != 1 {
		t.Fatalf("sole-check fallback must rewrite in place, not append: %+v", checks)
	}
	if checks[0].Source != "real.log" || checks[0].Assert != "matches ^OK" {
		t.Fatalf("the step's sole check must be rewritten to the equivalent, got %+v", checks[0])
	}
}

// A substitution with an empty assertion is ignored — no rewrite, no append. Substituting no check for
// a check is exactly the outcome the review is there to prevent.
func TestApplyCheckSubsSkipsEmptyAssertion(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "orig.log", Assert: "nonempty"}})
	a.applyCheckSubs(ctx, sid, []port.CheckSub{{Step: "1", Original: "nonempty", Source: "x", Assert: "   "}})
	checks := a.cachedChecks(sid)
	if len(checks) != 1 || checks[0].Source != "orig.log" || checks[0].Assert != "nonempty" {
		t.Fatalf("empty-assertion sub must be a no-op, got %+v", checks)
	}
}

// A solo agent's approved substitution rewrites its OWN session's checks (depth 0), and the pending
// queue is cleared so it is not re-reviewed.
func TestReviewSubstitutionsApproveSolo(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Assert: "matches listening"}}) // no source → no verdict
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "server.log",
		Assert: "matches ^listening on", Reason: "the check named no source"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds := 0
	act, looped := a.reviewSubstitutions(ctx, tc, &rounds, new(string))
	if looped {
		t.Fatalf("an approved substitution must not loop, got act=%v", act)
	}
	if got := a.cachedChecks(sid); got[0].Source != "server.log" {
		t.Fatalf("approved solo substitution should rewrite the check, got %+v", got[0])
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
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Assert: "matches listening"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "x.txt", Assert: "nonempty"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds := 0
	act, looped := a.reviewSubstitutions(ctx, tc, &rounds, new(string))
	if !looped || act != loopContinue {
		t.Fatalf("a critical concern must loop the agent, got act=%v looped=%v", act, looped)
	}
	if rounds != 1 {
		t.Fatalf("a correction round should be counted, rounds=%d", rounds)
	}
	if got := a.cachedChecks(sid); got[0].Assert != "matches listening" || got[0].Source != "" {
		t.Fatalf("a rejected substitution must NOT rewrite the check yet, got %+v", got[0])
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
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "server.log", Assert: "nonempty"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 1, maxSteps: 50} // depth>0 = worker
	rounds := 0
	if _, looped := a.reviewSubstitutions(ctx, tc, &rounds, new(string)); looped {
		t.Fatal("approved worker substitution must not loop")
	}
	got := a.takeApprovedSubs(sid)
	if len(got) != 1 || got[0].Source != "server.log" || got[0].Assert != "nonempty" {
		t.Fatalf("approved worker substitution should be stashed for the parent, got %+v", got)
	}
}

// Necessity guard: when the ORIGINAL check actually evaluates and PASSES here, the substitution is
// unneeded — it is dropped without convening the council, and the working original is kept unchanged.
func TestReviewSubstitutionsDropsUnneeded(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	// scriptPlatform reads back "verify output" with exit 0, so the original's assertion holds.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{0}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "out.log", Assert: "matches verify"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches verify", Source: "other.log",
		Assert: "nonempty", Reason: "works correctly"})

	s := a.sessionInfo(ctx, sid)
	if act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int), new(string)); looped {
		t.Fatalf("an unneeded substitution must not loop, got act=%v", act)
	}
	if fc.calls != 0 {
		t.Fatalf("an unneeded substitution must NOT convene the review council, calls=%d", fc.calls)
	}
	if got := a.cachedChecks(sid); got[0].Source != "out.log" {
		t.Fatalf("the working original must be kept, not rewritten, got %+v", got[0])
	}
	if len(a.pendingSubsOf(sid)) != 0 {
		t.Fatal("the unneeded substitution should be dropped")
	}
}

// Necessity guard: when the ORIGINAL check evaluates and FAILS, that is a real deliverable failure, not
// a broken check — the substitution is refused (not applied, council not convened) and the agent is
// looped to reconsider (fix the deliverable) instead of substituting the failure away.
func TestReviewSubstitutionsRefusesRanFailed(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	// The source reads back fine (exit 0) but does not carry what the assertion requires → a real failure.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{0}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "out.log", Assert: "matches MUST_APPEAR"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches MUST_APPEAR", Source: "out.log",
		Assert: "nonempty", Reason: "dodging"})

	s := a.sessionInfo(ctx, sid)
	act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int), new(string))
	if !looped || act != loopContinue {
		t.Fatalf("a ran-and-failed original must loop the agent to reconsider, got act=%v looped=%v", act, looped)
	}
	if fc.calls != 0 {
		t.Fatalf("a real failure must NOT convene the substitution council, calls=%d", fc.calls)
	}
	if got := a.cachedChecks(sid); got[0].Assert != "matches MUST_APPEAR" {
		t.Fatalf("a failing original must NOT be substituted away, got %+v", got[0])
	}
}

// The other failure direction: the source the step was supposed to record is NOT THERE. That is the
// deliverable's problem, not the check's — an absent source is exit 1, so the substitution is refused
// like any other real failure rather than treated as an unrunnable check to be swapped out.
func TestReviewSubstitutionsRefusesMissingSource(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	// cat exits non-zero: the recorded file is absent.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{1}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Source: "out.log", Assert: "nonempty"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "nonempty", Source: "somewhere-else.log", Assert: "nonempty"})

	s := a.sessionInfo(ctx, sid)
	act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int), new(string))
	if !looped || act != loopContinue {
		t.Fatalf("an absent recorded source must loop the agent, got act=%v looped=%v", act, looped)
	}
	if got := a.cachedChecks(sid); got[0].Source != "out.log" {
		t.Fatalf("the original must NOT be redirected at another file, got %+v", got[0])
	}
}

// A mixed report — one substitution whose original cannot be evaluated at all (justified) and one whose
// original evaluated and FAILED (must be refused) — loops the agent for the failure but KEEPS the
// justified substitution pending, so it is not lost.
func TestReviewSubstitutionsMixedKeepsJustified(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}}}}
	// Step 1's original names no source → 126 (no verdict, justified). Step 2's reads back and fails.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{codes: []int{0}}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Assert: "matches listening"},
		{Step: "2", Source: "out.log", Assert: "matches MUST_APPEAR"},
	})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "server.log", Assert: "nonempty"})
	a.addPendingSub(sid, port.CheckSub{Step: "2", Original: "matches MUST_APPEAR", Source: "out.log", Assert: "nonempty"})

	s := a.sessionInfo(ctx, sid)
	act, looped := a.reviewSubstitutions(ctx, turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}, new(int), new(string))
	if !looped || act != loopContinue {
		t.Fatalf("the ran-and-failed sub must loop the agent, got act=%v looped=%v", act, looped)
	}
	pend := a.pendingSubsOf(sid)
	if len(pend) != 1 || pend[0].Step != "1" {
		t.Fatalf("the justified (step 1) sub must be KEPT while the failure-masking (step 2) is dropped, got %+v", pend)
	}
}

// matchOriginalCheck resolves which STORED check a substitution is about. The worker quotes the item as
// it read it in the brief — which for a typed check is its assertion — so every handle must land on the
// same check, and a step whose checks are ambiguous with nothing quoted must NOT be guessed at.
func TestMatchOriginalCheck(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Source: "a.log", Assert: "nonempty"},
		{Step: "1", Source: "b.log", Assert: "matches ^OK"},
		{Step: "2", Source: "c.log", Assert: "matches ^DONE"},
	}
	for _, tc := range []struct {
		name string
		sub  port.CheckSub
		want string // source of the check that must be selected; "" = no match expected
	}{
		{"by identity", port.CheckSub{Step: "1", Original: checkIdent(checks[1])}, "b.log"},
		{"by assertion", port.CheckSub{Step: "1", Original: "nonempty"}, "a.log"},
		{"sole check in step", port.CheckSub{Step: "2"}, "c.log"},
		{"ambiguous, nothing quoted", port.CheckSub{Step: "1"}, ""},
		{"no such step", port.CheckSub{Step: "9", Original: "nonempty"}, ""},
	} {
		got, ok := matchOriginalCheck(checks, tc.sub)
		if tc.want == "" {
			if ok {
				t.Errorf("%s: want no match, got %+v", tc.name, got)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: want a match", tc.name)
			continue
		}
		if got.Source != tc.want {
			t.Errorf("%s: matched the wrong check, got source %q want %q", tc.name, got.Source, tc.want)
		}
	}
}

func TestCheckCommandUnrunnable(t *testing.T) {
	cases := []struct {
		out  string
		code int
		want bool
	}{
		{"", 127, true}, // command not found
		{"", 126, true}, // the check itself could not be evaluated
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

// The correction loop is BOUNDED: once the round budget (CouncilMaxRounds) is spent, the review drops
// the still-unapproved substitutions and PROCEEDS to finish — the terminal gate and external verifier
// are the backstop — instead of looping the agent forever. The council is not even consulted once the
// budget is gone (the top guard fires first).
func TestReviewSubstitutionsBudgetExhaustionProceeds(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	// A council that would keep rejecting — proving the BOUND, not the verdict, is what stops the loop.
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Continue,
		Verdicts: []council.Verdict{{Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "still weak"}}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Assert: "matches listening"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "x", Assert: "nonempty"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds := 3 // budget already spent
	act, looped := a.reviewSubstitutions(ctx, tc, &rounds, new(string))
	if looped || act != 0 {
		t.Fatalf("a spent budget must proceed (no further loop), got act=%v looped=%v", act, looped)
	}
	if len(a.pendingSubsOf(sid)) != 0 {
		t.Error("a budget-exhausted review must drop the unapproved pending substitutions")
	}
}

// A rejection is remembered: the critique is stored, and the NEXT review round is convened with it
// so a member judges whether its own objection was met. Previously the agent got the critique and
// the council did not, leaving each round free to raise a different objection.
func TestReviewSubstitutionsCarriesPriorCritique(t *testing.T) {
	t.Setenv("MAGI_SUBST_REVIEW", "1")
	ctx := context.Background()
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Decision: council.Continue, Verdicts: []council.Verdict{
			{Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "reaching the port is not serving a request"},
		}},
		{Decision: council.Done, Verdicts: []council.Verdict{{Decision: council.Done}}},
	}}
	// The original must stay unevaluable across BOTH rounds, or the second review would find the
	// substitution unjustified and never convene.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Assert: "matches listening"}})
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "server.log",
		Assert: "port_open 8080", Reason: "the check named no source"})

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, guard: newRunGuard(), depth: 0, maxSteps: 50}
	rounds, critique := 0, ""

	if _, looped := a.reviewSubstitutions(ctx, tc, &rounds, &critique); !looped {
		t.Fatal("a critical objection must loop the agent for a correction")
	}
	if !strings.Contains(critique, "not serving a request") {
		t.Fatalf("the rejection must be stored for the next round, got %q", critique)
	}
	if fc.reqs[0].Revision != "" {
		t.Errorf("the first round has no prior objection to carry, got %q", fc.reqs[0].Revision)
	}

	// Round two: same pending sub re-declared; the council must now SEE its own prior objection.
	a.addPendingSub(sid, port.CheckSub{Step: "1", Original: "matches listening", Source: "roundtrip.log",
		Assert: "matches ^ROUNDTRIP OK", Reason: "the check named no source"})
	if _, looped := a.reviewSubstitutions(ctx, tc, &rounds, &critique); looped {
		t.Fatal("an approved correction must not loop again")
	}
	if len(fc.reqs) < 2 {
		t.Fatalf("want a second deliberation, got %d", len(fc.reqs))
	}
	if !strings.Contains(fc.reqs[1].Revision, "not serving a request") {
		t.Errorf("the re-round must carry the prior objection:\n%s", fc.reqs[1].Revision)
	}
}
