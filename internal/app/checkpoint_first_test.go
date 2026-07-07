package app

import (
	"strings"
	"testing"
)

// checkpointFirstEnabled is opt-in: OFF unless explicitly turned on (unvalidated
// behavioral nudge). Mirrors the specFidelity flag test shape, but inverted default.
func TestCheckpointFirstEnabled(t *testing.T) {
	for _, v := range []string{"1", "on", "true", "yes", "ON", "True"} {
		t.Setenv("MAGI_CHECKPOINT_FIRST", v)
		if !checkpointFirstEnabled() {
			t.Errorf("MAGI_CHECKPOINT_FIRST=%q should enable checkpoint-first", v)
		}
	}
	for _, v := range []string{"", "0", "off", "false", "no", "garbage"} {
		t.Setenv("MAGI_CHECKPOINT_FIRST", v)
		if checkpointFirstEnabled() {
			t.Errorf("MAGI_CHECKPOINT_FIRST=%q should NOT enable (default is off)", v)
		}
	}
}

// The note and planner rule must carry the load-bearing ideas: build the check FIRST,
// reproduce the stated procedure, and include a named counter-example.
func TestCheckpointFirstConstants(t *testing.T) {
	for _, want := range []string{"checkpoint", "BEFORE", "counter-example"} {
		if !strings.Contains(checkpointFirstNote, want) {
			t.Errorf("checkpointFirstNote missing %q", want)
		}
	}
	for _, want := range []string{"CHECKPOINT FIRST", "EARLY step", "counter-example"} {
		if !strings.Contains(checkpointFirstRule, want) {
			t.Errorf("checkpointFirstRule missing %q", want)
		}
	}
}

// The criteria elicitation must ask for the HOW-to-confirm of execution-checkable
// conditions, so the contract is checkpoint-friendly.
func TestElicitCriteriaAsksHowToConfirm(t *testing.T) {
	for _, want := range []string{"confirmed by execution", "expected output", "verification procedure the task"} {
		if !strings.Contains(elicitCriteriaSystem, want) {
			t.Errorf("elicitCriteriaSystem missing %q", want)
		}
	}
}
