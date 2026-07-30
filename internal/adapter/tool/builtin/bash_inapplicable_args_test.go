package builtin

import (
	"context"
	"encoding/json"
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
	run := func(args map[string]any) string {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := Bash{}.Execute(context.Background(), b, port.ToolEnv{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			return string(res.Content)
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
