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

// The restraint clause is OFF by default and adds NOTHING when off.
//
// An A/B measures the clause only if the control arm's prompt is what it always was. Returning a
// header with no items, or a stray blank line, would make both arms different from the shipped
// prompt and the comparison would be against neither.
func TestRestraintIsOffAndCostsNothingUntilAskedFor(t *testing.T) {
	t.Setenv("MAGI_RESTRAINT", "")
	if got := restraintClause(); got != "" {
		t.Errorf("the clause is on by default, or leaves residue when off: %q", got)
	}

	t.Setenv("MAGI_RESTRAINT", "1")
	on := restraintClause()
	if on == "" {
		t.Fatal("MAGI_RESTRAINT=1 added nothing")
	}
	// The two things magi's prompt does NOT already ask for.
	for what, want := range map[string]string{
		"stating assumptions":     "assumptions you are working from",
		"asking when ambiguous":   "LIST the readings",
		"stopping when confused":  "name the confusing part",
		"no single-use abstract":  "abstraction for something used once",
		"no idle configurability": "configurability nobody asked for",
		"no impossible branch":    "case that cannot occur",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("the clause does not cover %s (%q)", what, want)
		}
	}
	// Checkbox shape, like the two gates beside it — this tree moved the heaviest obligations to
	// checklists because prose in the middle of a paragraph is what gets skipped.
	if n := strings.Count(on, "- [ ]"); n < 6 {
		t.Errorf("the clause has %d checkboxes; it is prose again", n)
	}
}

// It must not repeat what the prompt already demands. The same obligation in two wordings is how a
// prompt grows until the instruction that matters is the one nobody reads.
func TestRestraintDoesNotRestateWhatThePromptAlreadyAsks(t *testing.T) {
	t.Setenv("MAGI_RESTRAINT", "1")
	on := restraintClause()
	for _, already := range []string{
		"SMALLEST change", // step 3 already says it
		"Match the surrounding style",
		"REPRODUCE it first",
		"stray files",
	} {
		if strings.Contains(on, already) {
			t.Errorf("the clause repeats %q, which the prompt already demands", already)
		}
	}
}

// The SHIPPED prompt carries none of it.
//
// systemPrompt is built once at package init, so this reads the actual string a default run sends.
// The control arm of an A/B has to be the prompt as it was, or the comparison is against neither
// arm — and a clause that leaked in unflagged would make every measurement since meaningless.
func TestTheShippedPromptDoesNotCarryTheRestraintClause(t *testing.T) {
	for _, leak := range []string{
		"say the quiet part",
		"assumptions you are working from",
		"LIST the readings",
		"configurability nobody asked for",
	} {
		if strings.Contains(systemPrompt, leak) {
			t.Errorf("the default prompt carries %q — the clause is not off by default", leak)
		}
	}
}
