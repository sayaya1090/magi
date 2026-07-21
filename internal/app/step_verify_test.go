package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestLeadingInt(t *testing.T) {
	cases := map[string]int{
		"1":          1,
		"3.":         3,
		"step 4":     4,
		"STEP 2) go": 2,
		"12) many":   12,
		"":           -1,
		"add 2 file": -1, // letters before digits → not ordinal
		"build":      -1,
	}
	for in, want := range cases {
		if got := leadingInt(in); got != want {
			t.Errorf("leadingInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTodoTitle(t *testing.T) {
	if got := todoTitle("write parser — produces: parser.go"); got != "write parser" {
		t.Errorf("todoTitle strip = %q", got)
	}
	if got := todoTitle("  plain title  "); got != "plain title" {
		t.Errorf("todoTitle plain = %q", got)
	}
}

func TestMatchTodoIndex(t *testing.T) {
	td := []session.Todo{
		{Content: "Build the binary"},
		{Content: "Write tests — produces: foo_test.go"},
		{Content: "Document usage"},
	}
	cases := map[string]int{
		"2":                1, // ordinal
		"step 3":           2, // ordinal with prefix
		"build the binary": 0, // exact (case-insensitive) title
		"write tests":      1, // exact against pre-annotation title
		"document":         2, // substring (step ⊂ title)
		"nonexistent":      -1,
		"":                 -1,
		"9":                -1, // out of range ordinal
	}
	for step, want := range cases {
		if got := matchTodoIndex(td, step); got != want {
			t.Errorf("matchTodoIndex(%q) = %d, want %d", step, got, want)
		}
	}
}

// setChecks installs plan-audit checks directly into turn state (bypassing the flag
// gate in storePlanChecks so the gate itself is what the test exercises).
func setChecks(a *App, sid session.SessionID, checks []council.DeliverableCheck) {
	a.mu.Lock()
	a.stateLocked(sid).deliverableChecks = checks
	a.mu.Unlock()
}

// The gate stays inert when the flag is explicitly off, even with checks stored and a platform.
func TestRunStepGateInactiveWhenFlagOff(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "0") // default is on; this arm turns it off
	plat := &scriptPlatform{codes: []int{0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "true"}})
	ts := &turnState{}
	if got, _ := a.runStepGate(context.Background(), a.sessionInfo(context.Background(), sid), ts); got != gateInactive {
		t.Fatalf("flag off → %v, want gateInactive", got)
	}
	if plat.calls != 0 {
		t.Errorf("flag off must not run any check, ran %d", plat.calls)
	}
}

// All checks pass → gateInactive (council still judges — no skip), and the passing step's todo is
// checked off deterministically.
func TestRunStepGateAllPass(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	plat := &scriptPlatform{codes: []int{0}} // Exec → stdout "verify output", exit 0
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	a.putTodos(context.Background(), sid, plannerActor, []session.Todo{{Content: "do it", Status: "in_progress"}})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Command: "run", Expect: "verify"}})

	ts := &turnState{}
	if got, _ := a.runStepGate(context.Background(), a.sessionInfo(context.Background(), sid), ts); got != gateInactive {
		t.Fatalf("all-pass → %v, want gateInactive (council no longer skipped)", got)
	}
	td := a.Todos(sid)
	if td[0].Status != "completed" {
		t.Errorf("passing step todo status = %q, want completed", td[0].Status)
	}
}

// A failing check injects the diagnosis ONCE (gateFailRetry), then falls back to the
// council (gateInactive) on the next call — fire-once via ts.stepNudged.
func TestRunStepGateFailInjectsOnceThenFallsBack(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	plat := &scriptPlatform{codes: []int{1, 1}} // both calls fail
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	setChecks(a, sid, []council.DeliverableCheck{{Step: "1", Deliverable: "out.txt", Command: "run"}})

	ts := &turnState{}
	s := a.sessionInfo(context.Background(), sid)
	if got, _ := a.runStepGate(context.Background(), s, ts); got != gateFailRetry {
		t.Fatalf("first fail → %v, want gateFailRetry", got)
	}
	if !ts.stepNudged {
		t.Error("first fail must latch ts.stepNudged")
	}
	if got, _ := a.runStepGate(context.Background(), s, ts); got != gateInactive {
		t.Fatalf("second fail (already nudged) → %v, want gateInactive", got)
	}
}

// A step with several deliverables completes only when ALL of them pass; a step that
// fully passes in the same round is still checked off.
func TestRunStepGateMultipleDeliverablesPerStep(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	// Order of checks below: step1-a pass, step1-b FAIL, step2 pass.
	plat := &scriptPlatform{codes: []int{0, 1, 0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow"})
	a.putTodos(context.Background(), sid, plannerActor, []session.Todo{
		{Content: "step one", Status: "in_progress"},
		{Content: "step two", Status: "pending"},
	})
	setChecks(a, sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "a", Command: "ra", Expect: "verify"},
		{Step: "1", Deliverable: "b", Command: "rb", Expect: "verify"}, // fails via exit code
		{Step: "2", Deliverable: "c", Command: "rc", Expect: "verify"},
	})

	ts := &turnState{}
	if got, _ := a.runStepGate(context.Background(), a.sessionInfo(context.Background(), sid), ts); got != gateFailRetry {
		t.Fatalf("mixed → %v, want gateFailRetry (step 1 has a failing deliverable)", got)
	}
	td := a.Todos(sid)
	if td[0].Status == "completed" {
		t.Error("step 1 has a failing deliverable → must NOT be completed")
	}
	if td[1].Status != "completed" {
		t.Errorf("step 2 fully passed → want completed, got %q", td[1].Status)
	}
}

func TestAnnotateTodosWithDeliverables(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	a.putTodos(context.Background(), sid, plannerActor, []session.Todo{
		{Content: "Build binary"},
		{Content: "Write docs"},
	})
	a.annotateTodosWithDeliverables(context.Background(), sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "magi.bin", Command: "go build"},
		{Step: "2", Deliverable: "README.md", Command: "test -s README.md"},
	})
	td := a.Todos(sid)
	if td[0].Content != "Build binary — produces: magi.bin" {
		t.Errorf("todo[0] = %q", td[0].Content)
	}
	if td[1].Content != "Write docs — produces: README.md" {
		t.Errorf("todo[1] = %q", td[1].Content)
	}
	// Idempotent: a second annotate must not double-append.
	a.annotateTodosWithDeliverables(context.Background(), sid, []council.DeliverableCheck{
		{Step: "1", Deliverable: "magi.bin", Command: "go build"},
	})
	if got := a.Todos(sid)[0].Content; got != "Build binary — produces: magi.bin" {
		t.Errorf("re-annotate changed todo: %q", got)
	}
}
