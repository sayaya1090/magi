package builtin

import (
	"strings"
	"testing"
)

// `… | head -N` makes the head stage 141 whenever there is more output than N lines, and that is
// head doing its job. pipeStageNote knew it; the "all stages clean" the other notes read did not.
// So one truncated pipeline got silence from the note that reads stages and, beside it, "that
// command's own status is not reported here" — one 141, read two ways in the same result.
//
// Observed live on the most common shape there is: `grep -r … runtime/*.c | head -50`, twice in the
// first five minutes of a run.
func TestATruncatedPipelineIsClean(t *testing.T) {
	if !stagesClean([]int{sigPipeExit, 0}) {
		t.Error("a stage killed by the reader closing the pipe has a known status and did not fail")
	}
	if note := swallowingPipeNote(0, `grep -r "x" runtime/*.c | head -50`, stagesClean([]int{sigPipeExit, 0})); note != "" {
		t.Errorf("magi holds the head's status; claiming otherwise sends the agent to re-run it:\n%s", note)
	}
	if note := pipeStageNote(0, []int{sigPipeExit, 0}); note != "" {
		t.Errorf("and the stage note must stay silent too — the two must not disagree:\n%s", note)
	}
}

// A real failure upstream still reaches the agent, from both notes' point of view: the stage note
// names it, and the "clean" predicate must not swallow it.
func TestARealUpstreamFailureIsNotClean(t *testing.T) {
	if stagesClean([]int{128, 0}) {
		t.Fatal("128 is a failure, not a closed pipe")
	}
	note := pipeStageNote(0, []int{128, 0})
	if !strings.Contains(note, "128 → 0") || !strings.Contains(note, "FAILED") {
		t.Errorf("the failing stage must be named:\n%s", note)
	}
	// Mixed: one stage truncated, an earlier one genuinely broken.
	if stagesClean([]int{2, sigPipeExit, 0}) {
		t.Error("a truncated stage does not excuse a failed one")
	}
}

// A command with no pipeline resolved nothing, so nothing may be claimed resolved — the notes that
// say "this exit may not be the head's" must still be free to fire.
func TestNoPipelineResolvesNothing(t *testing.T) {
	for _, stages := range [][]int{nil, {}, {0}} {
		if stagesClean(stages) {
			t.Errorf("stages %v is not a pipeline magi resolved", stages)
		}
	}
	if note := swallowingPipeNote(0, `make world 2>&1 | tail -20`, stagesClean(nil)); note == "" {
		t.Error("with no stage information the truncating tail is still worth flagging")
	}
}
