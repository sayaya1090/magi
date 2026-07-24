package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// fakeVerifyPlat returns a fixed ExecResult+error for every Exec, so runVerifyCmd's
// exit-code mapping can be exercised in isolation (no real process).
type fakeVerifyPlat struct {
	res port.ExecResult
	err error
}

func (p fakeVerifyPlat) Exec(context.Context, port.Cmd) (port.ExecResult, error) { return p.res, p.err }
func (fakeVerifyPlat) ConfigDir() string                                         { return "" }
func (fakeVerifyPlat) DataDir() string                                           { return "" }
func (fakeVerifyPlat) TerminalCaps() port.TermCaps                               { return port.TermCaps{} }
func (fakeVerifyPlat) ProcessCPUTime(int) (time.Duration, bool)                  { return 0, false }

// TestRunVerifyCmdExitCodeMapping locks the shared deliverable-check runner's exit-code
// contract (every check path — council_gate, plan_execute, step_verify, subst_review,
// workflow — routes through it). The critical edge is the timeout kill: the platform
// swallows the process's ExitError and reports ExitCode -1 with a nil Go error, which must
// pass through as -1 ("can't verify", no verdict) and NEVER be mapped to a false fail — the
// whole point of the per-command check timeout. A genuine start failure (binary not found:
// a non-ExitError with ExitCode 0) is the one case mapped to exit 1.
func TestRunVerifyCmdExitCodeMapping(t *testing.T) {
	ctx := context.Background()

	// No platform (unit tests): cannot verify (-1), never a false fail.
	if out, code := (&App{}).runVerifyCmd(ctx, "/w", "x"); code != -1 {
		t.Fatalf("nil platform: code=%d out=%q; want -1", code, out)
	}

	cases := []struct {
		name         string
		res          port.ExecResult
		err          error
		wantCode     int
		wantContains string
	}{
		{"clean pass", port.ExecResult{Stdout: []byte("ok"), ExitCode: 0}, nil, 0, "ok"},
		{"non-zero exit (real fail)", port.ExecResult{Stdout: []byte("FAIL"), ExitCode: 2}, nil, 2, "FAIL"},
		// Timeout kill: platform swallows the ExitError → (ExitCode -1, nil). Pass through as -1.
		{"timeout kill → -1 passthrough", port.ExecResult{Stdout: []byte("partial"), ExitCode: -1}, nil, -1, "partial"},
		// Start failure (binary not found): non-ExitError err with ExitCode 0 → mapped to exit 1.
		{"start failure → exit 1", port.ExecResult{ExitCode: 0}, errors.New("exec: not found"), 1, "not found"},
		// Stderr is appended to stdout in the combined output.
		{"stderr appended", port.ExecResult{Stdout: []byte("out"), Stderr: []byte("err-detail"), ExitCode: 1}, nil, 1, "err-detail"},
	}
	for _, c := range cases {
		a := &App{plat: fakeVerifyPlat{res: c.res, err: c.err}}
		out, code := a.runVerifyCmd(ctx, "/w", "cmd")
		if code != c.wantCode || !strings.Contains(out, c.wantContains) {
			t.Errorf("%s: code=%d out=%q; want code=%d containing %q", c.name, code, out, c.wantCode, c.wantContains)
		}
	}
}
