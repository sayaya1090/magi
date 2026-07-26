package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestWorkerChecklist(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Deliverable: "proto exists", Source: "kv.proto", Assert: "matches service KV"},
		{Step: "2. [solo] gen", Deliverable: "stubs", Source: "kv_pb2.py", Assert: "nonempty"},
		{Step: "3", Deliverable: "server up", Assert: "port_open 5328"},
	}

	// Step 1 (idx 0): only its own check, rendered as what the GATE will do — read this path, apply
	// this assertion — because the worker's obligation is to produce the file, not to run the check.
	got := workerChecklist(checks, 0)
	if !strings.Contains(got, "the gate will read kv.proto and require: matches service KV") {
		t.Errorf("step 1's own check must be rendered as a read + assertion:\n%s", got)
	}
	if strings.Contains(got, "5328") || strings.Contains(got, "kv_pb2.py") {
		t.Errorf("other steps' checks must not leak into step 1:\n%s", got)
	}
	if !strings.Contains(got, "do not report done while any of them would fail") {
		t.Error("must carry the definition-of-done instruction")
	}

	// Step 2 (idx 1) matches "2. [solo] gen".
	if !strings.Contains(workerChecklist(checks, 1), "kv_pb2.py") {
		t.Error("step 2's check must match the '2. …' step tag")
	}

	// A check with an assertion but no source is a live probe, not a file read: it must render as a
	// bare requirement rather than claim the gate reads something.
	if got := workerChecklist(checks, 2); !strings.Contains(got, "the gate will require: port_open 5328") ||
		strings.Contains(got, "will read ") {
		t.Errorf("a sourceless assertion must render as a bare requirement:\n%s", got)
	}

	// The set IS step-labeled, but no check targets this step → empty, NOT the union of every
	// step's checks: flattening temporally-separate steps onto one worker yields a
	// jointly-unsatisfiable checklist (#224). The worker sees only its own step's checks.
	if got := workerChecklist(checks, 9); got != "" {
		t.Errorf("labeled set with no check for this step must give an empty checklist, got:\n%s", got)
	}

	// A wholly UNLABELED set keeps the lenient fallback: step attribution is impossible, so
	// over-inform the worker with all checks rather than drop a requirement.
	unlabeled := []council.DeliverableCheck{
		{Source: "a.log", Assert: "nonempty"},
		{Source: "b.log", Assert: "nonempty"},
	}
	all := workerChecklist(unlabeled, 3)
	for _, c := range []string{"a.log", "b.log"} {
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

// matchStepChecks must NOT let label "1" bleed into "10".."19": the separator-anchored match
// (exact, or want followed by '.'/' '/')') is what stops the gate from running a step-10 check
// against step 1 — a false failure that would re-plan a step that is actually fine. Lock the
// boundary in both directions (a "1" query rejects "10"/"11"; "10"/"11" match their own steps).
func TestMatchStepChecksNoPrefixBleed(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Command: "one"},
		{Step: "10", Command: "ten"},
		{Step: "1.2", Command: "one-sub"},
		{Step: "11) go", Command: "eleven"},
	}
	// step 1 (idx 0): only "1" and its "1.2" sub-step — never "10"/"11".
	got := matchStepChecks(checks, 0)
	if len(got) != 2 {
		t.Fatalf("step 1 should match '1' and '1.2' only, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Command == "ten" || c.Command == "eleven" {
			t.Errorf("step 1 must not match a '10'/'11' check: %+v", got)
		}
	}
	// step 10 (idx 9): only "10".
	if got := matchStepChecks(checks, 9); len(got) != 1 || got[0].Command != "ten" {
		t.Errorf("step 10 should match only '10', got %+v", got)
	}
	// step 11 (idx 10): the "11) go" form.
	if got := matchStepChecks(checks, 10); len(got) != 1 || got[0].Command != "eleven" {
		t.Errorf("step 11 should match '11) go', got %+v", got)
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

// The two things a worker cannot learn from inside its own session: that an item with no assertion
// gates NOTHING (so leaving it be silently drops the requirement), and that a wrong path is repairable
// rather than a reason to fail. The checklist has to say both, and name the tool that does the repair.
func TestWorkerChecklistMarksChecksTheGateWillRefuse(t *testing.T) {
	got := workerChecklist([]council.DeliverableCheck{
		{Step: "1", Deliverable: "suite passes", Source: "suite.log", Assert: "matches ^All tests passed"},
		{Step: "1", Deliverable: "binary runs"}, // authored with nothing to assert
	}, 0)
	if !strings.Contains(got, "this item carries no assertion, so the gate cannot verify it") {
		t.Errorf("an item with no assertion must be marked as verifying nothing:\n%s", got)
	}
	if strings.Count(got, "carries no assertion") != 1 {
		t.Errorf("only the unasserted item may be marked:\n%s", got)
	}
	for _, want := range []string{"substitute_check", "A silent workaround is LOST"} {
		if !strings.Contains(got, want) {
			t.Errorf("the checklist must name the repair path (missing %q):\n%s", want, got)
		}
	}

	// Every checklist — including one where nothing is wrong — must state the direction: the worker
	// performs the run and records its real output, the gate only reads.
	clean := workerChecklist([]council.DeliverableCheck{
		{Step: "1", Deliverable: "output recorded", Source: "/tmp/suite.log", Assert: "matches PASS"},
	}, 0)
	if strings.Contains(clean, "carries no assertion") {
		t.Errorf("a fully typed checklist must not carry the unasserted warning:\n%s", clean)
	}
	for _, want := range []string{"YOU RUN, THE CHECK READS", "REAL output", "Never hand-write the file"} {
		if !strings.Contains(clean, want) {
			t.Errorf("every checklist must state that the recorded file is the worker's own real output (missing %q):\n%s", want, clean)
		}
	}
}
