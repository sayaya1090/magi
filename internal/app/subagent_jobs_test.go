package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A running child is on the register from the moment it starts, and stays there after it ends so
// the strip that shows it can fade it out instead of having it vanish mid-turn.
func TestAChildIsVisibleWhileItRunsAndAfterItEnds(t *testing.T) {
	var r subagentJobs
	r.start("child-1", "seele_plan", "plan the refactor")

	got := r.list()
	if len(got) != 1 || !got[0].Running {
		t.Fatalf("a child that just started is not listed as running: %+v", got)
	}
	if got[0].Tool != "seele_plan" || got[0].Task != "plan the refactor" {
		t.Errorf("the pane would say %q / %q", got[0].Tool, got[0].Task)
	}

	r.note("child-1", "spawn · step 1 · read")
	r.finish("child-1", 7, "")

	got = r.list()
	if len(got) != 1 {
		t.Fatalf("the child disappeared the moment it ended: %+v", got)
	}
	if got[0].Running {
		t.Error("a finished child is still marked running — its pane would never stop spinning")
	}
	if got[0].Steps != 7 {
		t.Errorf("Steps = %d, want 7", got[0].Steps)
	}
	if !strings.Contains(got[0].Tail, "step 1 · read") {
		t.Errorf("the progress did not reach the pane: %q", got[0].Tail)
	}
}

// The register is bounded. A loop-engineering plugin can start a great many children in one turn,
// and a map that only grows is a defect this tree has already paid for.
func TestTheRegisterDropsOldFinishedChildrenAndNeverARunningOne(t *testing.T) {
	var r subagentJobs
	r.start("long-runner", "looper", "still going")
	for i := 0; i < subagentJobKeep*3; i++ {
		id := session.SessionID("c" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		r.start(id, "looper", "round")
		r.finish(id, 1, "")
	}
	got := r.list()
	if len(got) > subagentJobKeep+1 { // +1 for the runner, which is never dropped
		t.Errorf("the register grew to %d entries on a cap of %d", len(got), subagentJobKeep)
	}
	var sawRunner bool
	for _, j := range got {
		if j.ID == "long-runner" {
			sawRunner = true
			if !j.Running {
				t.Error("the running child was marked finished")
			}
		}
	}
	if !sawRunner {
		t.Error("a child that is still running was evicted — its pane would never end")
	}
}

// The tail is bounded too, and cut at a line boundary: a pane opening on half a line reads as
// corrupted output rather than as a window onto a longer log.
func TestTheTailIsBoundedAndCutAtALineBoundary(t *testing.T) {
	var r subagentJobs
	r.start("c", "looper", "t")
	for i := 0; i < 400; i++ {
		r.note("c", strings.Repeat("x", 40)+" line")
	}
	tail := r.list()[0].Tail
	if len(tail) > subagentJobTailKeep+64 {
		t.Errorf("the tail grew to %d bytes on a cap of %d", len(tail), subagentJobTailKeep)
	}
	if strings.HasPrefix(tail, "x") && !strings.HasPrefix(tail, strings.Repeat("x", 40)) {
		t.Errorf("the tail starts mid-line: %.60q", tail)
	}
	// The NEWEST lines are the ones kept.
	if !strings.HasSuffix(strings.TrimRight(tail, "\n"), " line") {
		t.Errorf("the tail does not end on the most recent line: %.60q", tail)
	}
}
