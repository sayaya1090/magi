package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// "Condition met" is decided by the condition's exit code, and for a pipeline that exit belongs to
// the LAST stage. The bash tool states the per-stage statuses on every call because of it; wait_for
// — the tool whose entire job is deciding whether something succeeded — measured nothing.
//
// Observed live (fix-ocaml-gc, 2026-07-30):
//
//	condition met after 1s (1 checks): cd /app/ocaml && git fetch --depth=1 origin master 2>&1 | tail -3
//	fatal: couldn't find remote ref master
//
// The fetch failed and `tail` exited 0, so the wait ended one second in and reported success with
// the failure printed underneath it. The verdict stands — exit 0 is what the condition asked to be
// waited on — but the numbers behind it must be on the result too.
func TestWaitForStatesTheStagesBehindItsVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PIPESTATUS is a bash feature")
	}
	env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
	run := func(cond string) string {
		res, err := WaitFor{}.Execute(context.Background(),
			json.RawMessage(`{"condition":`+mustJSON(cond)+`,"timeout":5,"interval":1}`), env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		return s
	}

	// The live shape: the work fails, the pager succeeds, the wait ends.
	got := run("false 2>&1 | tail -3")
	if !strings.Contains(got, "condition met") {
		t.Fatalf("exit 0 is what the condition asked to be waited on:\n%s", got)
	}
	if !strings.Contains(got, "stages exited 1 → 0") {
		t.Errorf("the numbers behind the verdict must be on the result:\n%s", got)
	}

	// A single command is not a pipeline and has nothing to add.
	if got := run("true"); strings.Contains(got, "stages exited") {
		t.Errorf("one stage is not a pipeline:\n%s", got)
	}
	// A pipeline that succeeded throughout says nothing either — the note is for a disagreement.
	if got := run("echo hi | cat"); strings.Contains(got, "stages exited") {
		t.Errorf("every stage passed; there is no discrepancy to name:\n%s", got)
	}
	// And a condition that never comes true carries its last probe's stages, for the same reason.
	if got := run("false | grep nothing"); !strings.Contains(got, "condition not met") {
		t.Errorf("a failing condition times out:\n%s", got)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
