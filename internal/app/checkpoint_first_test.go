package app

import (
	"strings"
	"testing"
)

// checkpointFirstEnabled is ON by default: only an explicit falsey MAGI_CHECKPOINT_FIRST
// suppresses it (the A/B knob). Mirrors the specFidelity flag test shape.
func TestCheckpointFirstEnabled(t *testing.T) {
	for _, v := range []string{"", "1", "on", "true", "yes", "ON", "True", "garbage"} {
		t.Setenv("MAGI_CHECKPOINT_FIRST", v)
		if !checkpointFirstEnabled() {
			t.Errorf("MAGI_CHECKPOINT_FIRST=%q should enable checkpoint-first (default is on)", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_CHECKPOINT_FIRST", v)
		if checkpointFirstEnabled() {
			t.Errorf("MAGI_CHECKPOINT_FIRST=%q should NOT enable", v)
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
