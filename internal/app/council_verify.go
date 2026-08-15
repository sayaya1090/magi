package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// councilVerify runs the operator's fixed verification command — the harness the agent cannot
// subvert — at the finish gate, and returns evidence for the members plus an authoritative verdict.
//
// This is the answer to a gate that trusts the agent's own exit-0: the command is magi's, run by
// magi from config the agent's workspace edits cannot change, and its result is a FACT, not a claim.
// A non-zero exit refuses the finish whatever the members vote. Its one blind spot is a suite the
// agent disabled so the command still exits 0 (a TestMain that runs nothing); for a `go test`
// command magi additionally checks that tests actually executed and refuses a pass that ran none.
//
// ok=true only when the command passed AND (for go test) really ran tests. evidence is always a
// block to prepend to what the members see, headed so they know magi ran it.
func (a *App) councilVerify(ctx context.Context, workdir string) (evidence string, ok bool, ran bool) {
	cmd := strings.TrimSpace(a.cfg.CouncilVerify)
	if cmd == "" {
		return "", true, false // no fixed harness configured — nothing to add, nothing to veto
	}
	out, code := a.runVerifyCmd(ctx, workdir, cmd)
	head := "── INDEPENDENT VERIFICATION (magi ran `" + cmd + "`) ──\n"
	if code != 0 {
		// A non-zero exit is a real failure the agent cannot argue away. -1 means magi could not
		// run the check (no platform, a timeout) — not a pass, so it does not accept either, but it
		// is reported as "could not verify" rather than "failed".
		if code == -1 {
			return head + "could not be run (no result) — the finish is not verified.\n" + clipLine(out, 2000), false, true
		}
		return head + "FAILED (exit " + itoa(int64(code)) + "). This refuses the declared finish.\n" + clipLine(out, 2000), false, true
	}
	// Passed. If it is a `go test`, exit 0 alone is not proof: the two ways a frozen failing test
	// gets neutered both leave exit 0 —
	//   1. a TestMain that never calls m.Run (runs nothing, so exit 0 means nothing), and
	//   2. a TestMain that runs the tests, sees them fail, and forces os.Exit(0) to mask it.
	// A re-run under -json exposes both: it emits a per-test event only when a test genuinely runs
	// (catches 1 as ran==0) AND a `fail` event for every test that failed before the exit was masked
	// (catches 2 as failed>0). Either one refuses the finish, whatever the masked exit said.
	if isGoTest(cmd) {
		ran, failed := a.goTestsExecuted(ctx, workdir)
		if ran == 0 {
			return head + "passed (exit 0) but executed NO tests — the suite is empty or has been " +
				"disabled (e.g. a TestMain that skips everything). A pass that ran nothing does not " +
				"verify the work; the finish is not accepted on this evidence.\n" + clipLine(out, 2000), false, true
		}
		if failed > 0 {
			return head + "passed (exit 0) but " + itoa(int64(failed)) + " test(s) FAILED when re-run " +
				"under -json — the exit code was forced (e.g. a TestMain that runs the tests and then " +
				"os.Exit(0)s over the failure). The tests themselves did not pass; the finish is not " +
				"accepted on this evidence.\n" + clipLine(out, 2000), false, true
		}
	}
	return head + "passed (exit 0).\n" + clipLine(out, 2000), true, true
}

var goTestRe = regexp.MustCompile(`\bgo\s+test\b`)

func isGoTest(cmd string) bool { return goTestRe.MatchString(cmd) }

// goTestsExecuted re-runs `go test -json ./...` in the workdir and reports how many test functions
// actually ran and how many of those failed. Uses -json because a neutering TestMain still prints
// `ok <pkg>` on the plain output, indistinguishable from a real pass; -json emits a per-test event
// only when a test genuinely runs, and a `fail` event for a test that failed even if the process
// then exits 0. ran==0 means nothing ran (empty or disabled suite); failed>0 means a test failed
// behind a masked exit. ran==-1 means magi could not run the check at all.
//
// go test exits non-zero when a test fails, so a non-zero exit / Exec error is EXPECTED here and is
// not treated as "could not run": the per-test events on stdout are the evidence. Only an outright
// absence of output with an error is "could not run".
func (a *App) goTestsExecuted(ctx context.Context, workdir string) (ran, failed int) {
	if a.plat == nil {
		return -1, 0
	}
	// Bound the re-run like runVerifyCmd bounds its command: a workspace test that hangs would
	// otherwise wedge the finish gate for the whole turn wall-clock, since only the turn context
	// reaches here. A timeout kills it and we report what events arrived before the kill.
	if d := checkCmdTimeout(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "go", Args: []string{"test", "-json", "./..."}, Dir: workdir})
	if strings.TrimSpace(string(res.Stdout)) == "" && err != nil {
		return -1, 0
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass", "fail":
			ran++
			if ev.Action == "fail" {
				failed++
			}
		}
	}
	return ran, failed
}
