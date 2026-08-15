package app

import (
	"context"
	"errors"
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

// A `go test` whose exit was FORCED to 0 over a real failure — the second neutering (a TestMain
// that runs the tests, sees them fail, and os.Exit(0)s) — is refused. The -json re-run still carries
// the per-test `fail` event, so the gate sees the failure the masked exit hid. Reverting the
// failed>0 check in councilVerify accepts (ok=true) this finish.
func TestVerifyRefusesAGoTestMaskingAFailure(t *testing.T) {
	// call 0 = the verify command, masked to exit 0. call 1 = the -json re-run, also exit 0 (the
	// mask), but its stdout carries a failing test event and a passing one — the failure the mask hid.
	plat := &scriptPlatform{
		codes: []int{0, 0},
		outs: []string{"", `{"Action":"run","Test":"TestA"}
{"Action":"fail","Test":"TestA"}
{"Action":"run","Test":"TestB"}
{"Action":"pass","Test":"TestB"}`},
	}
	a, _, wd := newWorkflowApp(t, nil, plat, Config{Permission: "allow", CouncilVerify: "go test ./..."})

	evidence, ok, ran := a.councilVerify(context.Background(), wd)
	if ok {
		t.Fatal("a go test that masked a failure with os.Exit(0) verified ok — the mask was accepted")
	}
	if !ran {
		t.Error("ran should be true — the command did run")
	}
	if !strings.Contains(evidence, "FAILED") {
		t.Errorf("the evidence does not name the masked failure:\n%s", evidence)
	}
}

// A `go test` that exits 0 but whose -json re-run magi could NOT run (no output, an error) is not
// verified — exit 0 is unconfirmed, not a pass. Reverting the ran<0 guard false-accepts it.
func TestVerifyDoesNotAcceptWhenTheReRunCannotRun(t *testing.T) {
	// call 0 = verify command exits 0; call 1 = the -json re-run errors with no output → ran==-1.
	plat := &scriptPlatform{codes: []int{0, 0}, errs: []error{nil, errors.New("go: cannot find main module")}}
	a, _, wd := newWorkflowApp(t, nil, plat, Config{Permission: "allow", CouncilVerify: "go test ./..."})

	evidence, ok, ran := a.councilVerify(context.Background(), wd)
	if ok {
		t.Fatal("a go test whose -json re-run could not run was accepted as verified")
	}
	if !ran {
		t.Error("ran should be true — the verify command itself did run")
	}
	if !strings.Contains(evidence, "could not re-run") {
		t.Errorf("the evidence does not explain the unconfirmed pass:\n%s", evidence)
	}
}

// The -json re-run scope is taken from the operator's own command, not a hardcoded ./..., so it
// tests the same packages/flags — otherwise it false-refuses a finish the operator's narrower scope
// passed, and false-passes the empty-suite guard when the module has other tests.
func TestGoTestReRunArgsMatchTheOperatorScope(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"go test ./...", []string{"test", "-json", "./..."}},
		{"go test ./pkg/...", []string{"test", "-json", "./pkg/..."}},
		{"go test -short -tags integration ./svc/...", []string{"test", "-json", "-short", "-tags", "integration", "./svc/..."}},
		{"  go test  ", []string{"test", "-json"}},                      // no package: go's own default (the workdir pkg)
		{"make test", []string{"test", "-json", "./..."}},               // wrapped: fall back
		{"cd sub && go test ./...", []string{"test", "-json", "./..."}}, // shell: fall back
		{"go test ./... | tee out", []string{"test", "-json", "./..."}}, // pipe: fall back
		{"gotestsum -- ./...", []string{"test", "-json", "./..."}},      // not `go test`: fall back
	} {
		got := goTestReRunArgs(c.cmd)
		if len(got) != len(c.want) {
			t.Errorf("%q → %v, want %v", c.cmd, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q → %v, want %v", c.cmd, got, c.want)
				break
			}
		}
	}
}
