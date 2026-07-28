package app

import (
	"context"
	"strings"
	"testing"
)

// A turn that wrote a runnable file and never ran it is the one thing magi can establish cheaply
// and without reading a line of the file. The nudge used to live inside the council gate and went
// out with it; this pins it to the finish path itself, where it does not depend on a council
// existing at all.
func TestAnUnexercisedArtifactIsNamedOnceAtTheFinish(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.cfg.Workflow = false
	ctx := context.Background()
	g := newRunGuard()
	g.recordChange("/app/run.py", "", "print(1)\n")
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: g}
	ts := &turnState{}

	act, done := a.nudgeUnexercised(ctx, tc, ts)
	if !done || act != loopContinue {
		t.Fatalf("a written-but-never-run file must hold the finish once, got act=%v done=%v", act, done)
	}
	txt := sessionText(t, a, sid)
	if !strings.Contains(txt, "/app/run.py") || !strings.Contains(txt, "no executed command naming") {
		t.Errorf("the nudge must name the file and say what the record holds:\n%s", txt)
	}
	// Once. It is a report, and repeating a report the agent has already answered is nagging.
	if _, done := a.nudgeUnexercised(ctx, tc, ts); done {
		t.Error("the nudge must fire at most once per turn")
	}

	// A file the record DOES have a command for is not named — the ledger matches a module loaded
	// by stem too, so `python3 -c "from run import …"` counts as running run.py.
	g2 := newRunGuard()
	g2.recordChange("/app/run.py", "", "print(1)\n")
	g2.noteBashExec(`python3 -c "from run import go"`, true)
	tc2 := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: g2}
	if _, done := a.nudgeUnexercised(ctx, tc2, &turnState{}); done {
		t.Error("a file the record shows exercised must not be named")
	}
}
