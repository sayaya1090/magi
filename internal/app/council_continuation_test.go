package app

import (
	"strings"
	"testing"
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

// When the attempt ledger is non-empty, the CONTINUE injection lists the dead-ends and tells
// the agent to diverge; an empty ledger adds nothing.
func TestContinuationTextInjectsTriedLedger(t *testing.T) {
	got := continuationText("do X", "task", "- fail grep foo → no match")
	if !strings.Contains(got, "do NOT repeat") || !strings.Contains(got, "grep foo") {
		t.Errorf("continuation must inject the tried-ledger digest; got:\n%s", got)
	}
	clean := continuationText("do X", "task", "")
	if strings.Contains(clean, "do NOT repeat") {
		t.Errorf("no tried block should appear for an empty ledger; got:\n%s", clean)
	}
}
