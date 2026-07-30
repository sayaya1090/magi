package builtin

import (
	"strconv"
	"strings"
	"testing"
)

// The stage note states numbers. It used to read them, and the reading was wrong three times in one
// hour of live runs: a stage killed by SIGPIPE (`… | head -50` produces 141 every time, because head
// exited after fifty lines) called a failure; grep exiting 1 because nothing matched called a
// failure; and the exemption list that fixed those two needing to know which verb produced which
// status — a question with no answer in `a | b || c | d`, where the tail runs only when the head
// fails and either could be the pipeline PIPESTATUS captured.
//
// The model reads shell output for a living and can see the body. What it cannot see is the status
// the shell threw away, so magi hands it that and says nothing about what it means.
func TestTheStageNoteStatesTheNumbersAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		what   string
		stages []int
	}{
		{"a truncated pipe", []int{141, 0}},
		{"grep matching nothing", []int{1, 1, 0}},
		{"a difference found, then truncated", []int{1, 141, 0}},
		{"a build that died", []int{2, 0}},
		{"not a git repository", []int{128, 0}},
	} {
		note := pipeStageNote(c.stages[len(c.stages)-1], c.stages)
		if note == "" {
			t.Errorf("%s: the discarded statuses are exactly what the exit cannot show", c.what)
			continue
		}
		for _, st := range c.stages {
			if !strings.Contains(note, strconv.Itoa(st)) {
				t.Errorf("%s: every stage's status must appear:\n%s", c.what, note)
			}
		}
		// No verdict: whether 141 or 1 or 2 means trouble is the reader's call, not magi's.
		for _, word := range []string{"FAILED", "failed", "success", "error"} {
			if strings.Contains(note, word) {
				t.Errorf("%s: the note reads the statuses instead of stating them (%q):\n%s",
					c.what, word, note)
			}
		}
	}
}

// Silent where the reported status is already the whole story: nothing was hidden, and a note on
// every pipe is noise the model pays for on every call.
func TestTheStageNoteIsSilentWhenNothingWasHidden(t *testing.T) {
	for _, c := range []struct {
		what   string
		exit   int
		stages []int
	}{
		{"every stage agrees with the exit", 0, []int{0, 0}},
		{"the last stage failed on its own and says so", 1, []int{0, 1}},
		{"…however it failed", 2, []int{0, 0, 2}},
		{"a single command is not a pipeline", 0, []int{0}},
		{"no statuses were captured", 0, nil},
	} {
		if note := pipeStageNote(c.exit, c.stages); note != "" {
			t.Errorf("%s: %q", c.what, note)
		}
	}
}

// The two notes that say "this status may not be the one you want" exist for the case magi could not
// resolve. Once it holds the per-stage statuses it states them, and repeating "that command's own
// status is not reported here" beside the numbers would be false — it was just reported.
func TestTheAmbiguityNotesYieldToTheFacts(t *testing.T) {
	const piped = `make world 2>&1 | tail -20`
	if note := swallowingPipeNote(0, piped, false); note == "" {
		t.Error("with no stage information the truncating tail is exactly what to flag")
	}
	if note := swallowingPipeNote(0, piped, true); note != "" {
		t.Errorf("the statuses are stated above; claiming they are not is false:\n%s", note)
	}
	const tailed = `ls boot/ocamlrun 2>&1 || echo "not found"`
	if note := maskingTailNote(0, tailed, false); note == "" {
		t.Error("a `|| …` tail leaves the exit genuinely ambiguous")
	}
	if note := maskingTailNote(0, tailed, true); note != "" {
		t.Errorf("resolved by fact, so the sentence is redundant:\n%s", note)
	}
}
