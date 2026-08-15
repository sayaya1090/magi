package app

import (
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
}

// forget drops exactly one child, whatever its state, and leaves the rest of the register intact —
// this is the meeting-close reap. Unlike eviction it does not wait for the cap and does not spare a
// running one (a closing room's participant is finished by the time it is forgotten, but forget is
// state-agnostic on purpose). A forget of an absent id is a harmless no-op with no order/byID
// desync, so a double close cannot corrupt the register.
func TestForgetDropsExactlyOneChildAndIsSafeToRepeat(t *testing.T) {
	var r subagentJobs
	r.start("keep", "looper", "still here")
	r.start("reap", "meeting", "in the room")
	r.finish("reap", 3, "")

	r.forget("reap")
	got := r.list()
	if len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("forget did not drop exactly the one child: %+v", got)
	}

	// Idempotent: forgetting it again, and forgetting an id that was never registered, do nothing.
	r.forget("reap")
	r.forget("never-existed")
	if got := r.list(); len(got) != 1 || got[0].ID != "keep" {
		t.Errorf("a repeated / absent forget disturbed the register: %+v", got)
	}

	// State-agnostic: a still-running child can be forgotten (the reap does not wait for it to end).
	r.forget("keep")
	if got := r.list(); len(got) != 0 {
		t.Errorf("forget did not drop a running child: %+v", got)
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
