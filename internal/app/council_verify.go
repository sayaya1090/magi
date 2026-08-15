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
	// Passed. If it is a `go test`, make sure it did not pass by running nothing — the exact way a
	// frozen failing test gets neutered (a TestMain that exits 0 without m.Run).
	if isGoTest(cmd) {
		if n := a.goTestsExecuted(ctx, workdir); n == 0 {
			return head + "passed (exit 0) but executed NO tests — the suite is empty or has been " +
				"disabled (e.g. a TestMain that skips everything). A pass that ran nothing does not " +
				"verify the work; the finish is not accepted on this evidence.\n" + clipLine(out, 2000), false, true
		}
	}
	return head + "passed (exit 0).\n" + clipLine(out, 2000), true, true
}

var goTestRe = regexp.MustCompile(`\bgo\s+test\b`)

func isGoTest(cmd string) bool { return goTestRe.MatchString(cmd) }

// goTestsExecuted counts how many test functions actually ran under `go test -json ./...` in the
// workdir. Zero means the package either has no tests or has been neutered so nothing runs — either
// way a `go test` that "passes" without running a test has verified nothing. Uses -json because a
// neutering TestMain still prints `ok <pkg>` on the plain output, indistinguishable from a real
// pass; -json emits a per-test event only when a test genuinely runs.
func (a *App) goTestsExecuted(ctx context.Context, workdir string) int {
	if a.plat == nil {
		return -1
	}
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "go", Args: []string{"test", "-json", "./..."}, Dir: workdir})
	if err != nil {
		return -1
	}
	n := 0
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Test != "" &&
			(ev.Action == "pass" || ev.Action == "fail") {
			n++
		}
	}
	return n
}
