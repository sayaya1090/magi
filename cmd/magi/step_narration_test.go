package main

import (
	"strings"
	"testing"
)

// The default is the wording every recorded run was produced under. Changing what the model writes
// on every step is an A/B, not a silent default, so an unset env must leave the prompt byte-identical.
func TestTheDefaultWordingIsUnchanged(t *testing.T) {
	t.Setenv("MAGI_TERSE_STEPS", "")
	const was = "Keep the user informed as you go, ask before destructive or irreversible actions, and stay concise."
	if got := stepNarrationClause(); got != was {
		t.Errorf("default wording changed:\n got %q\nwant %q", got, was)
	}
}

// On, it drops the pre-announcement and keeps the obligation. "Keep the user informed as you go" on
// a loop that asks for the next step every step reads as "write a line before each tool call", and
// that is what 96% of the assistant text in the recorded runs is — next to a transcript that already
// shows every call and its result.
func TestTerseStepsDropsThePreAnnouncementAndKeepsTheDuty(t *testing.T) {
	t.Setenv("MAGI_TERSE_STEPS", "1")
	got := stepNarrationClause()
	if strings.Contains(got, "as you go") {
		t.Errorf("the phrase that produced the per-step line is still there:\n%s", got)
	}
	for _, want := range []string{
		"do NOT announce your next step",   // the behaviour being removed
		"the tool output does not already", // what is still worth saying
		"final summary",                    // the summary step must survive
		"destructive or irreversible",      // the confirmation duty must survive
		"concise",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Only an explicit on-value flips it: a stray empty/garbage value must not silently change what
// every run's model is told.
func TestOnlyAnExplicitOnValueFlipsIt(t *testing.T) {
	for _, v := range []string{"", "0", "off", "false", "no", "maybe"} {
		t.Setenv("MAGI_TERSE_STEPS", v)
		if strings.Contains(stepNarrationClause(), "do NOT announce") {
			t.Errorf("%q must not enable it", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", "YES", " on "} {
		t.Setenv("MAGI_TERSE_STEPS", v)
		if !strings.Contains(stepNarrationClause(), "do NOT announce") {
			t.Errorf("%q must enable it", v)
		}
	}
}

// The clause is spliced into the prompt, so a mistake in the concatenation would be invisible until
// a run. It must appear exactly once, with the sections around it intact.
func TestTheClauseIsSplicedIntoThePrompt(t *testing.T) {
	if n := strings.Count(systemPrompt, "ask before destructive or irreversible actions"); n != 1 {
		t.Errorf("clause appears %d times in the built prompt", n)
	}
	for _, want := range []string{"6. DECLARE IT", "# Persistence (don't give up)", "LANGUAGE (important)"} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("the splice lost %q", want)
		}
	}
}
