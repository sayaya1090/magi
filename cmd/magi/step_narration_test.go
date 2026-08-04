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

// The two steps that carry the most obligations are checklists, not paragraphs. They were one
// dense block each, and the instruction most often missed on this benchmark — deliver a signal
// the task names for REAL instead of simulating it in-process — sat in the middle of the longer
// one. A task whose whole grade turns on that (cancel-async-tasks: 10 passes in 54 runs, every
// failure the same in-process substitute) had to find it there.
//
// This pins the SHAPE. Prose can carry the same words and be skimmed past; a box that cannot be
// ticked is a question the model has to answer. If someone reflows these back into sentences,
// this fails and says why.
func TestTheTwoHeaviestStepsAreChecklists(t *testing.T) {
	for _, gate := range []string{
		"### Pre-flight — confirm each of these before your FIRST edit",
		"### Verify gate — every line applies, none may be skipped",
	} {
		if !strings.Contains(systemPrompt, gate) {
			t.Errorf("systemPrompt lost the gate heading %q", gate)
		}
	}
	// Every obligation of the verify gate is its own box. Counting them keeps a rewrite from
	// quietly folding several back into one line.
	verify := systemPrompt[strings.Index(systemPrompt, "### Verify gate"):]
	verify = verify[:strings.Index(verify, "5. SUMMARIZE")]
	if n := strings.Count(verify, "\n- [ ] "); n < 8 {
		t.Errorf("verify gate has %d boxes, want the 8 obligations kept separate", n)
	}
	// The specific rule that gets missed, and the reason it is not optional.
	for _, want := range []string{
		"send the ACTUAL signal",
		"Simulating it in-process",
		"does not count",
		"RUN the checkpoint yourself and SEEN it pass",
		"Never weaken or replace it",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("the external-event rule lost %q", want)
		}
	}
	// Pre-flight is a gate on the FIRST edit, not a post-hoc review.
	if !strings.Contains(systemPrompt, "investigation you owe now, not after the edit") {
		t.Error("pre-flight must say when it is owed")
	}
}
