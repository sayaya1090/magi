package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The coverage-fill prompt must carry the coverage contract: fill gaps with a SUPERSET (existing
// unchanged + one new check per uncovered producing step), scope each new check by step number, keep
// checks portable + idempotent, and skip read-only steps. Guards the criteria.go prompt against a
// silent regression that would let the fill drop checks or invent work-doing "checks".
func TestCoverageFillPromptCarriesContract(t *testing.T) {
	for _, want := range []string{
		"FILLING GAPS", "existing checks UNCHANGED", "one NEW check for EACH producing step",
		"1-based number", "IDEMPOTENT", "read-only step", "Do NOT alter or drop the existing checks",
	} {
		if !strings.Contains(coverageFillSystem, want) {
			t.Errorf("coverageFillSystem must state the coverage contract (missing %q)", want)
		}
	}
}

// With the flag off, ensureStepCoverage is a pure passthrough — the authored checks are used as-is and
// no fill call is made, even when the plan has more steps than covered checks (a real gap).
func TestEnsureCoverageFlagOffPassthrough(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "0")
	// A reply that WOULD add coverage, to prove the flag short-circuits before any call is used.
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a"},{"step":"2","command":"b"}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	if len(out) != 1 || out[0].Command != "a" {
		t.Fatalf("flag off must return the authored checks unchanged, got %+v", out)
	}
}

// When every plan step is already covered (covered distinct steps >= plan steps), there is no gap, so
// ensureStepCoverage returns the input untouched and makes no fill call.
func TestEnsureCoverageNoGap(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	a := newOrchApp(t, &gateLLM{text: `[{"step":"9","command":"junk"}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}, {Step: "2", Command: "b"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	if len(out) != 2 {
		t.Fatalf("no gap must return the authored checks unchanged, got %d checks: %+v", len(out), out)
	}
}

// The core case: an 11-step plan with a single authored check is a coverage gap; the fill pass replaces
// it with the superset the reviewer returns (existing + new per-step checks), so the termination gate's
// ledger now spans the whole plan instead of one step.
func TestEnsureCoverageFillsGap(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	filled := `[{"step":"1","command":"a"},{"step":"2","command":"b"},{"step":"3","command":"c"}]`
	a := newOrchApp(t, &gateLLM{text: filled}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	covered := map[string]bool{}
	for _, c := range out {
		covered[strings.TrimSpace(c.Step)] = true
	}
	if len(out) != 3 || len(covered) != 3 {
		t.Fatalf("gap must be filled to a 3-step superset, got %d checks over %d steps: %+v", len(out), len(covered), out)
	}
}

// A reply that is NOT a coverage-increasing superset (it drops an authored check, or adds no distinct
// step) is rejected — the authored contract is kept rather than weakened. Two sub-cases.
func TestEnsureCoverageRejectsNonSuperset(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}}

	// (a) reply drops the existing check (fewer than authored) → keep authored.
	a := newOrchApp(t, &gateLLM{text: `[]`}, Config{Permission: "allow", MaxAgents: 10})
	if out := a.ensureStepCoverage(context.Background(), s, "task", steps, in); len(out) != 1 {
		t.Errorf("a reply dropping checks must keep the authored set, got %+v", out)
	}

	// (b) reply is bigger but adds no NEW distinct step (both checks on step 1) → no coverage gained.
	a2 := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a"},{"step":"1","command":"a2"}]`}, Config{Permission: "allow", MaxAgents: 10})
	if out := a2.ensureStepCoverage(context.Background(), s, "task", steps, in); len(out) != 1 {
		t.Errorf("a reply adding no distinct-step coverage must keep the authored set, got %+v", out)
	}
}

// The 0-step solo path: no authored checks and a single synthetic step for the objective is a gap, so
// the fill authors one check — solo work lands a verifiable contract instead of an empty ledger.
func TestEnsureCoverageSoloObjective(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","deliverable":"server on 5328","command":"python3 -c \"import socket;exit(0 if socket.socket().connect_ex(('localhost',5328))==0 else 1)\""}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	steps := []planStep{{Title: "run a server on port 5328", Strategy: "solo"}}
	out := a.ensureStepCoverage(context.Background(), s, "run a server on port 5328", steps, nil)
	if len(out) != 1 || strings.TrimSpace(out[0].Command) == "" {
		t.Fatalf("solo objective must get one authored check, got %+v", out)
	}
}
