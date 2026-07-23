package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestWorkerChecklist(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Deliverable: "proto exists", Command: "test -f kv.proto"},
		{Step: "2. [solo] gen", Deliverable: "stubs", Command: "test -f kv_pb2.py"},
		{Step: "3", Command: "python server.py"},
	}

	// Step 1 (idx 0): only its own check, framed as a must-run acceptance checklist.
	got := workerChecklist(checks, 0)
	if !strings.Contains(got, "test -f kv.proto") {
		t.Error("step 1's own check must be included")
	}
	if strings.Contains(got, "server.py") || strings.Contains(got, "kv_pb2.py") {
		t.Errorf("other steps' checks must not leak into step 1:\n%s", got)
	}
	if !strings.Contains(got, "before you report done") {
		t.Error("must carry the run-and-verify instruction")
	}

	// Step 2 (idx 1) matches "2. [solo] gen".
	if !strings.Contains(workerChecklist(checks, 1), "kv_pb2.py") {
		t.Error("step 2's check must match the '2. …' step tag")
	}

	// The set IS step-labeled, but no check targets this step → empty, NOT the union of every
	// step's checks: flattening temporally-separate steps onto one worker yields a
	// jointly-unsatisfiable checklist (plexus #224). The worker sees only its own step's checks.
	if got := workerChecklist(checks, 9); got != "" {
		t.Errorf("labeled set with no check for this step must give an empty checklist, got:\n%s", got)
	}

	// A wholly UNLABELED set keeps the lenient fallback: step attribution is impossible, so
	// over-inform the worker with all checks rather than drop a requirement.
	unlabeled := []council.DeliverableCheck{
		{Command: "test -f a"},
		{Command: "test -f b"},
	}
	all := workerChecklist(unlabeled, 3)
	for _, c := range []string{"test -f a", "test -f b"} {
		if !strings.Contains(all, c) {
			t.Errorf("unlabeled set should fall back to all checks; missing %q", c)
		}
	}

	if workerChecklist(nil, 0) != "" {
		t.Error("no checks → empty checklist")
	}
}

// stepChecks filters by the 1-based step label and falls back to all when nothing matches —
// the structured basis both workerChecklist and the TUI's SubagentChecklist share.
func TestStepChecks(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Command: "a"},
		{Step: "2. gen", Command: "b"},
		{Step: "2)", Command: "c"},
	}
	if got := stepChecks(checks, 1); len(got) != 2 || got[0].Command != "b" || got[1].Command != "c" {
		t.Errorf("step 2 (idx 1) should match '2. gen' and '2)': %+v", got)
	}
	if got := stepChecks(checks, 0); len(got) != 1 || got[0].Command != "a" {
		t.Errorf("step 1 (idx 0) should match only '1': %+v", got)
	}
	// Labeled set, no check for this step → empty (never the contradictory union of all steps).
	if got := stepChecks(checks, 7); got != nil {
		t.Errorf("labeled set with no match → nil, not the union, got %+v", got)
	}
	// Wholly unlabeled set → lenient fallback returns all (step attribution impossible).
	unlabeled := []council.DeliverableCheck{{Command: "a"}, {Command: "b"}}
	if got := stepChecks(unlabeled, 7); len(got) != 2 {
		t.Errorf("unlabeled set → fall back to all, got %d", len(got))
	}
	if stepChecks(nil, 0) != nil {
		t.Error("no checks → nil")
	}
}

// anyStepLabeled is true iff at least one check carries a numeric step label — the switch that
// makes stepChecks honor labels strictly (no flatten-all) rather than over-inform.
func TestAnyStepLabeled(t *testing.T) {
	if !anyStepLabeled([]council.DeliverableCheck{{Command: "a"}, {Step: "2. gen", Command: "b"}}) {
		t.Error("a set with one numeric label must count as labeled")
	}
	if anyStepLabeled([]council.DeliverableCheck{{Step: "", Command: "a"}, {Step: "cleanup", Command: "b"}}) {
		t.Error("empty and title-only step labels are not numeric labels")
	}
	if anyStepLabeled(nil) {
		t.Error("no checks → not labeled")
	}
}

// SubagentChecklist resolves a child session to its parent plan step's deliverable checks; a
// child with no parent/step, or an unknown session, yields nothing.
func TestSubagentChecklist(t *testing.T) {
	a := curateApp(t)
	step := 1
	parent := session.SessionID("s_parent")
	child := session.SessionID("s_child")
	a.mu.Lock()
	a.stateLocked(parent).deliverableChecks = []council.DeliverableCheck{
		{Step: "1", Command: "step0"},
		{Step: "2", Deliverable: "the step-2 artifact", Command: "step1"},
	}
	a.stateLocked(child).meta = session.Session{ID: child, Parent: parent, ParentStep: &step}
	a.stateLocked(parent).meta = session.Session{ID: parent}
	a.mu.Unlock()

	got := a.SubagentChecklist(child)
	if len(got) != 1 || got[0].Command != "step1" {
		t.Fatalf("child on step idx 1 must get step-2's check, got %+v", got)
	}
	// A child with no plan-step link → nothing.
	orphan := session.SessionID("s_orphan")
	a.mu.Lock()
	a.stateLocked(orphan).meta = session.Session{ID: orphan, Parent: parent}
	a.mu.Unlock()
	if got := a.SubagentChecklist(orphan); got != nil {
		t.Errorf("child with no ParentStep must yield nil, got %+v", got)
	}
	if got := a.SubagentChecklist("nope"); got != nil {
		t.Errorf("unknown session must yield nil, got %+v", got)
	}
}

// CouncilContract returns the turn's acceptance criteria and deliverable checks — the ledger the
// council detail view surfaces.
func TestCouncilContract(t *testing.T) {
	a := curateApp(t)
	sid := session.SessionID("s_main")
	a.mu.Lock()
	a.stateLocked(sid).criteria = "the server responds on :5328"
	a.stateLocked(sid).deliverableChecks = []council.DeliverableCheck{{Step: "1", Command: "curl :5328"}}
	a.mu.Unlock()
	crit, checks := a.CouncilContract(sid)
	if crit != "the server responds on :5328" || len(checks) != 1 || checks[0].Command != "curl :5328" {
		t.Fatalf("contract = %q %+v", crit, checks)
	}
	if crit, checks := a.CouncilContract("nope"); crit != "" || checks != nil {
		t.Errorf("unknown session must yield empty contract, got %q %+v", crit, checks)
	}
}
