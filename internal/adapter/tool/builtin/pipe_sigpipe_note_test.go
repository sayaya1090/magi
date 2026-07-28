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
	if !stagesClean("grep x f | head -50", []int{sigPipeExit, 0}) {
		t.Error("a stage killed by the reader closing the pipe has a known status and did not fail")
	}
	if note := swallowingPipeNote(0, `grep -r "x" runtime/*.c | head -50`, stagesClean("grep x f | head -50", []int{sigPipeExit, 0})); note != "" {
		t.Errorf("magi holds the head's status; claiming otherwise sends the agent to re-run it:\n%s", note)
	}
	if note := pipeStageNote("grep x f | head -50", 0, []int{sigPipeExit, 0}); note != "" {
		t.Errorf("and the stage note must stay silent too — the two must not disagree:\n%s", note)
	}
}

// A real failure upstream still reaches the agent, from both notes' point of view: the stage note
// names it, and the "clean" predicate must not swallow it.
func TestARealUpstreamFailureIsNotClean(t *testing.T) {
	if stagesClean("git log | head", []int{128, 0}) {
		t.Fatal("128 is a failure, not a closed pipe")
	}
	note := pipeStageNote("git log | head", 0, []int{128, 0})
	if !strings.Contains(note, "128 → 0") || !strings.Contains(note, "FAILED") {
		t.Errorf("the failing stage must be named:\n%s", note)
	}
	// Mixed: one stage truncated, an earlier one genuinely broken.
	if stagesClean("make | grep x | head", []int{2, sigPipeExit, 0}) {
		t.Error("a truncated stage does not excuse a failed one")
	}
}

// A command with no pipeline resolved nothing, so nothing may be claimed resolved — the notes that
// say "this exit may not be the head's" must still be free to fire.
func TestNoPipelineResolvesNothing(t *testing.T) {
	for _, stages := range [][]int{nil, {}, {0}} {
		if stagesClean("a | b", stages) {
			t.Errorf("stages %v is not a pipeline magi resolved", stages)
		}
	}
	if note := swallowingPipeNote(0, `make world 2>&1 | tail -20`, stagesClean("make world 2>&1 | tail -20", nil)); note == "" {
		t.Error("with no stage information the truncating tail is still worth flagging")
	}
}

// grep answers with its exit: 1 means "no line matched", not "grep broke". Reporting it as a
// failure is the same mistake SIGPIPE was, one convention over.
//
// Observed live, five minutes into a run:
// `cd /app/ocaml/runtime && grep -n "free_list\|Free_block" *.c | grep -v "test" | head -50`
// returned 0 bytes — nothing matched — and carried "the work at the head of the pipe FAILED".
func TestGrepFindingNothingIsAnAnswerNotAFailure(t *testing.T) {
	const cmd = `cd /app/ocaml/runtime && grep -n "free_list\|Free_block" *.c | grep -v "test" | head -50`
	if got := lastPipelineVerbs(cmd); len(got) != 3 || got[0] != "grep" || got[2] != "head" {
		t.Fatalf("the last pipeline is grep | grep -v | head, got %q", got)
	}
	if !stagesClean(cmd, []int{1, 1, 0}) {
		t.Error("two greps finding nothing is two answers, not two failures")
	}
	if note := pipeStageNote(cmd, 0, []int{1, 1, 0}); note != "" {
		t.Errorf("nothing failed here:\n%s", note)
	}
	// grep's 2 IS an error (unreadable file, bad pattern) and must survive.
	if stagesClean(cmd, []int{2, 1, 0}) {
		t.Error("grep exit 2 is a real error")
	}
	// Only the verbs that answer this way: `make` exiting 1 is a broken build.
	if stagesClean("make world | head -50", []int{1, 0}) {
		t.Error("make exit 1 is a failure")
	}
}

// An operator inside quotes is data. A splitter that miscounts hands stage 0's verb to stage 1's
// status, so the count is checked and a mismatch means no verb is claimed at all.
func TestPipelineSplitHonorsQuotes(t *testing.T) {
	if got := lastPipelineVerbs(`grep -n "a|b" f.c | head`); len(got) != 2 || got[0] != "grep" {
		t.Errorf("the alternation is inside the pattern: %q", got)
	}
	if got := lastPipelineVerbs(`a && b; c | d`); len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("only the LAST pipeline has captured statuses: %q", got)
	}
	if got := lastPipelineVerbs(`make 2>&1 | tail -5`); len(got) != 2 || got[0] != "make" {
		t.Errorf("2>&1 is a redirection, not a background operator: %q", got)
	}
	// A parse that disagrees with the statuses claims nothing: two verbs, three statuses.
	if stagesClean(`grep x f | head`, []int{1, 1, 0}) {
		t.Error("with the count mismatched no verb may excuse a status")
	}
}
