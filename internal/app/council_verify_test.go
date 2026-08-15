package app

import (
	"context"
	"strings"
	"testing"
)

// A finish the members accept but the fixed verification fails is refused. The harness runs the
// operator's command, magi owns the result, and a non-zero exit vetoes the vote — the deterministic
// floor an agent cannot argue past.
func TestVerifyVetoesAFinishTheMembersAccepted(t *testing.T) {
	fc := &recordingCouncil{}                // votes Done
	plat := &scriptPlatform{codes: []int{1}} // the verify command exits non-zero
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilVerify: "run-the-checks"})
	a.cfg.Workflow = false
	ctx := context.Background()

	out, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.takeTurnControl(sid).finish {
		t.Fatal("a finish whose verification FAILED ended the turn anyway")
	}
	if !(strings.Contains(out, "REFUSED") && strings.Contains(out, "verification")) {
		t.Errorf("the refusal does not name the verification:\n%s", out)
	}
}

// A `go test` that passes but ran no tests — the frozen-suite neutering (a TestMain that skips
// everything) — is refused: a pass that executed nothing verifies nothing.
func TestVerifyRefusesAGoTestThatRanNoTests(t *testing.T) {
	fc := &recordingCouncil{} // votes Done
	// Both the verify run and the -json count return exit 0 with non-JSON output → zero tests.
	plat := &scriptPlatform{codes: []int{0, 0}}
	a, sid, _ := newWorkflowApp(t, nil, plat, Config{Permission: "allow", Council: fc, CouncilVerify: "go test ./..."})
	a.cfg.Workflow = false
	ctx := context.Background()

	out, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.takeTurnControl(sid).finish {
		t.Fatal("a go test that ran no tests ended the turn — the neutered-suite trick was accepted")
	}
	if !strings.Contains(out, "REFUSED") {
		t.Errorf("the refusal is not surfaced:\n%s", out)
	}
}
