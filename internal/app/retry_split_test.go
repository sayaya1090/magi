package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// partitionStepChecks RUNS the step's checks and splits them by real result: passing checks'
// deliverables in passed, failing ones as ledger lines in fails. Here step 1 has two checks — the
// first exits 0 (pass), the second exits 1 (fail) — so the split is one each and active is true.
func TestPartitionStepChecksSplits(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	plat := &scriptPlatform{codes: []int{0, 1}} // check-a passes, check-b fails
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "server responds", Command: "probe-a"},
		{Step: "1", Deliverable: "cleanup done", Command: "probe-b"},
	})
	s := a.sessionInfo(context.Background(), sid)

	passed, fails, active := a.partitionStepChecks(context.Background(), s, 0)
	if !active {
		t.Fatal("checks present + platform + flag on → active")
	}
	if len(passed) != 1 || passed[0] != "server responds" {
		t.Errorf("passed = %v, want [server responds]", passed)
	}
	if len(fails) != 1 || !strings.Contains(fails[0], "cleanup done") {
		t.Errorf("fails = %v, want one line mentioning cleanup done", fails)
	}
}

// With no checks tagged for the step, there is nothing to run: active is false so the caller falls
// back to a non-check strategy.
func TestPartitionStepChecksInactiveNoChecks(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	s := a.sessionInfo(context.Background(), sid)

	if _, _, active := a.partitionStepChecks(context.Background(), s, 0); active {
		t.Error("no checks → inactive")
	}
	if plat.calls != 0 {
		t.Errorf("nothing to run, but ran %d checks", plat.calls)
	}
}

// retryContinuation, with executable checks, hands the retry a SKIP/CONTINUE split by real disk
// state: the passing deliverable under "ALREADY SATISFIED", the failing one under "STILL UNMET".
func TestRetryContinuationSplitBlock(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_RETRY_SPLIT", "1")
	plat := &scriptPlatform{codes: []int{0, 1}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "server responds", Command: "probe-a"},
		{Step: "1", Deliverable: "cleanup done", Command: "probe-b"},
	})
	s := a.sessionInfo(context.Background(), sid)

	got := a.retryContinuation(context.Background(), s, 0, port.SpawnResult{})
	for _, want := range []string{"do NOT restart", "ALREADY SATISFIED", "server responds", "STILL UNMET", "cleanup done"} {
		if !strings.Contains(got, want) {
			t.Errorf("continuation missing %q\n---\n%s", want, got)
		}
	}
}

// With the split flag off, retryContinuation falls back to the generic pivot digest — it must name
// the previous failure and demand a different route, never re-hand the identical brief silently.
func TestRetryContinuationFlagOffFallsBackToPivot(t *testing.T) {
	t.Setenv("MAGI_RETRY_SPLIT", "0")
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "x", Command: "probe"}})
	s := a.sessionInfo(context.Background(), sid)

	got := a.retryContinuation(context.Background(), s, 0, port.SpawnResult{Err: "worker crashed"})
	if strings.Contains(got, "ALREADY SATISFIED") {
		t.Error("flag off must NOT produce the check split")
	}
	if !strings.Contains(got, "worker crashed") || !strings.Contains(got, "DIFFERENT route") {
		t.Errorf("fallback must carry the failure + demand a pivot:\n%s", got)
	}
}

// No checks for the step (flag on) → the deterministic split is unavailable, so it also falls back
// to the pivot digest rather than returning nothing.
func TestRetryContinuationNoChecksFallsBackToPivot(t *testing.T) {
	t.Setenv("MAGI_RETRY_SPLIT", "1")
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	s := a.sessionInfo(context.Background(), sid)

	got := a.retryContinuation(context.Background(), s, 0, port.SpawnResult{Err: "empty result"})
	if !strings.Contains(got, "DIFFERENT route") {
		t.Errorf("no-check fallback must be the pivot digest:\n%s", got)
	}
}
