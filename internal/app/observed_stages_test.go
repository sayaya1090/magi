package app

import (
	"strings"
	"testing"
)

// The record used to learn "the head of the pipe failed" by scanning the note for that sentence.
// The note stopped making the claim, and the scan went quiet — no test failed, the record simply
// stopped carrying the fact. Numbers instead: the record states the stage statuses it was given.
func TestTheRecordCarriesTheStageStatuses(t *testing.T) {
	const res = "exit 0\n[note: the status above is the pipeline's LAST stage. Its stages exited 2 → 0 (left to right).]\nbuilding"
	got := stagesOfBashResult(res)
	if len(got) != 2 || got[0] != 2 || got[1] != 0 {
		t.Fatalf("both statuses must survive, got %v", got)
	}
	if n := stagesOfBashResult("exit 0\nnothing to see"); n != nil {
		t.Errorf("a result with no stage note yields nothing, got %v", n)
	}
	// Three stages, and a shape magi does not recognize.
	if got := stagesOfBashResult("Its stages exited 1 → 141 → 0 (left to right)"); len(got) != 3 || got[1] != 141 {
		t.Errorf("three statuses, verbatim: %v", got)
	}

	o := observedRun{cmds: []observedCmd{
		{cmd: "make world | tail -50", exit: 0, stages: []int{2, 0}},
		{cmd: "go test ./...", exit: 1},
		{cmd: "ls", exit: 0},
	}}
	txt := o.render()
	if !strings.Contains(txt, "stages 2 0") {
		t.Errorf("the pipeline's statuses belong in the record:\n%s", txt)
	}
	if !strings.Contains(txt, "→ exit 1") {
		t.Errorf("a plain command still shows its own exit:\n%s", txt)
	}
	for _, verdict := range []string{"FAILED", "failed", "clean"} {
		if strings.Contains(txt, verdict) {
			t.Errorf("the record states, it does not read (%q):\n%s", verdict, txt)
		}
	}
}
