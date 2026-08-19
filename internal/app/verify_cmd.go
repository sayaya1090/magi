package app

// The verification helpers: run a command, and — for a `go test` — tell a real pass from one that
// exited 0 without running anything.
//
// These outlived the council gate they were written for. That gate ran an operator-configured
// command at every completion declaration and vetoed the finish on a non-zero exit, and it was
// removed because the guarantee did not survive contact with what it was for: the command is
// fixed, so it answers one question ("did anything I already had break") however much the task
// changes, and the part of a suite that WOULD cover the new work is written by the agent this
// turn — self-graded again. What it reliably caught was regression, which is what a build script
// and CI already run.
//
// The workflow's verify phase keeps them, and needs the go-test check for its own reason: its
// command is often auto-detected from a Makefile or package.json that the implement phase can
// write, so exit 0 on its own proves even less there.

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

var goTestRe = regexp.MustCompile(`\bgo\s+test\b`)

func isGoTest(cmd string) bool { return goTestRe.MatchString(cmd) }

// goTestReRunArgs derives the -json re-run's arguments from the operator's OWN verify command, so the
// re-run covers the same packages and flags the operator chose. A hardcoded `./...` tested a
// different scope than the command: it false-REFUSED a finish the operator's `go test ./pkg/...`
// passed (a failure in another package under ./...), and it false-passed the empty-suite guard when
// the operator's scope was narrower than the module (some other package had tests). Only a plain
// `go test …` invocation is parsed — a command wrapped in a shell (a pipe, &&, ;, cd, a substitution,
// make/gotestsum) falls back to `./...`, since its real scope can't be read off the string. `go test`
// with no package keeps go's own default (the workdir package), matching the operator.
func goTestReRunArgs(cmd string) []string {
	c := strings.TrimSpace(cmd)
	fallback := []string{"test", "-json", "./..."}
	if strings.ContainsAny(c, "|&;<>`$(){}\n") || strings.Contains(c, "cd ") {
		return fallback
	}
	fields := strings.Fields(c)
	if len(fields) < 2 || fields[0] != "go" || fields[1] != "test" {
		return fallback
	}
	return append([]string{"test", "-json"}, fields[2:]...)
}

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
func (a *App) goTestsExecuted(ctx context.Context, workdir, cmd string) (ran, failed int) {
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
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "go", Args: goTestReRunArgs(cmd), Dir: workdir})
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
