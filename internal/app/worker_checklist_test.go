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

	// No check tagged for this step → fall back to ALL (don't let the worker skip anything).
	all := workerChecklist(checks, 9)
	for _, c := range []string{"kv.proto", "kv_pb2.py", "server.py"} {
		if !strings.Contains(all, c) {
			t.Errorf("untagged step should fall back to all checks; missing %q", c)
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
	if got := stepChecks(checks, 7); len(got) != 3 {
		t.Errorf("no match → fall back to all, got %d", len(got))
	}
	if stepChecks(nil, 0) != nil {
		t.Error("no checks → nil")
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
