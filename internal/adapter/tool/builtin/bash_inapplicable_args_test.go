package builtin

import (
	"context"
	"encoding/json"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// `timeout` and `pty` are both declared on the bash tool, and each applies to only ONE of its two
// modes. The branch that does not use one used to return without a word about it.
//
// The background branch already discloses the other thing it silently changes — the redundant `&`
// it strips — so the rule was already there; the timeout was just outside it. Observed live
// (large-scale-text-editing, 2026-07-30): `timeout:5` with `background:true`, and the job was
// still running when the agent gave up and killed it at 2m18s.
func TestBashSaysWhichArgumentItDidNotApply(t *testing.T) {
	// ⚠ **A background job outlives the call — including the one that made it.**
	//
	// This test starts `sleep 30` detached, twice, to check what the tool SAYS about a timeout it
	// did not apply. Saying it is the whole point, so the jobs are real and they keep running after
	// Execute returns. Nothing stopped them: two sleepers per run, holding the workdir the test is
	// about to delete.
	//
	// On Unix that is invisible — a directory unlinks with handles still open in it. On Windows the
	// removal fails, and it fails in Cleanup, so the test's own assertions all pass and the test
	// still reports FAIL with a message about `unlinkat`. Measured 2026-09-10.
	//
	// So whatever is started here is stopped here, by the door the message itself names.
	env := port.ToolEnv{Workdir: t.TempDir()}
	run := func(args map[string]any) string {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := Bash{}.Execute(context.Background(), b, env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		for _, m := range regexp.MustCompile(`bg_\d+`).FindAllString(s, -1) {
			id := m
			t.Cleanup(func() {
				_, _ = BashKill{}.Execute(context.Background(),
					json.RawMessage(`{"id":"`+id+`"}`), env)
			})
		}
		return s
	}

	// timeout + background: the job outlives the call, so there is nothing for a foreground
	// deadline to bound. Say so, and name what WOULD bound it.
	out := run(map[string]any{"command": "sleep 30", "background": true, "timeout": 5})
	if !strings.Contains(out, "started background command") {
		t.Fatalf("the job still starts: %s", out)
	}
	for _, want := range []string{"`timeout` of 5s was NOT applied", "bash_kill", "timeout 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}

	// No timeout given, no claim about one.
	if out := run(map[string]any{"command": "sleep 30", "background": true}); strings.Contains(out, "NOT applied") {
		t.Errorf("nothing was dropped, so nothing may say it was:\n%s", out)
	}

	// pty without background: the foreground path never reads it, so the caller asked for a
	// terminal and got a pipe. It matters because the programs that need a tty are exactly the
	// ones that hang on a prompt nothing can answer.
	out = run(map[string]any{"command": "echo hi", "pty": true})
	if !strings.Contains(out, "`pty` only applies with background=true") {
		t.Errorf("an ignored pty must be reported:\n%s", out)
	}
	if !strings.Contains(out, "bash_input") {
		t.Errorf("and the note names the route that would work:\n%s", out)
	}

	// A plain foreground command says neither.
	out = run(map[string]any{"command": "echo hi"})
	if strings.Contains(out, "pty") || strings.Contains(out, "NOT applied") {
		t.Errorf("nothing was ignored here:\n%s", out)
	}

	// pty WITH background is honored (where the platform has one), so it is not reported as
	// ignored either way.
	out = run(map[string]any{"command": "sleep 30", "background": true, "pty": true})
	if strings.Contains(out, "only applies with background=true") {
		t.Errorf("pty was applicable here:\n%s", out)
	}
	if ptySupported && runtime.GOOS != "windows" && !strings.Contains(out, "pseudo-terminal") {
		t.Errorf("a granted pty says so:\n%s", out)
	}
}
