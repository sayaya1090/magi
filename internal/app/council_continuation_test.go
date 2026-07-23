package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The CONTINUE injection must re-anchor the agent on the verbatim objective and carry
// the completion-audit rubric — over a long turn a weak model paraphrases the spec and
// narrows success, so the raw feedback alone is not enough to hold it to the full task.
func TestContinuationTextReanchorsObjectiveAndAudits(t *testing.T) {
	task := "implement get_val(key) returning the stored value verbatim"
	fb := "the value is not returned for missing keys"
	got := continuationText(fb, task, "")

	// The round's feedback is present.
	if !strings.Contains(got, fb) {
		t.Errorf("continuation must carry the round feedback; got:\n%s", got)
	}
	// The exact objective is re-anchored verbatim (anti-paraphrase / anti-scope-narrowing).
	if !strings.Contains(got, task) {
		t.Errorf("continuation must re-anchor the verbatim objective; got:\n%s", got)
	}
	if !strings.Contains(got, "verbatim") || !strings.Contains(got, "do not narrow") {
		t.Errorf("continuation must forbid narrowing/paraphrasing the objective; got:\n%s", got)
	}
	// The completion audit forces evidence over intent.
	for _, want := range []string{"treat 'done' as UNPROVEN", "current-state evidence", "NOT done"} {
		if !strings.Contains(got, want) {
			t.Errorf("continuation must include audit clause %q; got:\n%s", want, got)
		}
	}
	// The keep-work guard (no rebuild) still rides along.
	if !strings.Contains(got, "not a rebuild") {
		t.Errorf("continuation must keep the no-rebuild guard; got:\n%s", got)
	}
}

// The CONTINUE injection must offer the removal-only rebuttal: the agent may CONTEST a
// demand that is already met or impossible-as-stated instead of churning, but the
// affordance must state the evidence bar and that a contest is not itself a done claim
// (so it never reopens false-done — the council still decides done).
func TestContinuationTextOffersContest(t *testing.T) {
	got := continuationText("confirm exactly one server process via ps", "run a server on port 5328", "")

	if !strings.Contains(got, "CONTEST:") {
		t.Errorf("continuation must offer the CONTEST affordance; got:\n%s", got)
	}
	// Removal-only + evidence bar + not-a-done-claim guards must all be present.
	if !strings.Contains(got, "impossible exactly as stated") {
		t.Error("contest affordance must cover the impossible-as-stated case (absent tool)")
	}
	if !strings.Contains(got, "is NOT a claim of done") {
		t.Error("contest affordance must state it does not finish the task (false-done guard)")
	}
	if !strings.Contains(got, "is ignored") {
		t.Error("contest affordance must state the evidence bar (no-evidence contest is ignored)")
	}
}

// An empty objective must not emit an empty "Original objective:" stub — the re-anchor
// block is skipped entirely, but the audit still rides.
func TestContinuationTextSkipsEmptyObjective(t *testing.T) {
	got := continuationText("do X", "   ", "")
	if strings.Contains(got, "Original objective") {
		t.Errorf("no objective block should appear for a blank task; got:\n%s", got)
	}
	if !strings.Contains(got, "Completion audit") {
		t.Errorf("audit must still ride when the objective is blank; got:\n%s", got)
	}
}

// When the plan-audit froze deliverable checks, the CONTINUE injection carries them as
// a closed acceptance contract — binding the fallback council to the frozen scope so it
// cannot invent new demands (§3). An empty contract leaves the injection untouched.
func TestContinuationTextBindsFrozenContract(t *testing.T) {
	a := &App{}
	contract := a.frozenContractClause([]council.DeliverableCheck{
		{Step: "1", Deliverable: "parser.go", Command: "go build ./..."},
		{Step: "2", Deliverable: "tests green", Command: "go test ./..."},
	})
	got := continuationText("fix it", "objective", contract)
	for _, want := range []string{"Acceptance contract", "must NOT add new", "parser.go", "go test ./..."} {
		if !strings.Contains(got, want) {
			t.Errorf("continuation must carry frozen contract clause %q; got:\n%s", want, got)
		}
	}
	// No frozen checks → no contract block (baseline untouched).
	if strings.Contains(continuationText("fix it", "objective", ""), "Acceptance contract") {
		t.Error("empty contract must not emit the acceptance-contract block")
	}
}
