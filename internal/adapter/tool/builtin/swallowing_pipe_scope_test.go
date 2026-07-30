package builtin

import (
	"strings"
	"testing"
)

// A truncator only swallows the exit code when it is the command's LAST stage. Its tail may not run
// past a separator into another command — sequencedTail's body has that exclusion; this one, its
// sibling, did not.
//
// Observed live (sqlite-with-gcov, 2026-07-30):
//
//	cat /etc/environment | head -1 && echo "PATH=…" > /tmp/p && cat /tmp/p
//
// matched from `| head -1` to the end of the string, and the note told the agent its exit 0 belonged
// to ` head -1 && echo "PATH=…" > /tmp/p && cat /tmp/p` — a "stage" spanning two `&&` operators,
// which is not a thing. The exit belonged to the final `cat`, a real command whose status the result
// DOES report, so the right answer was to say nothing.
func TestATruncatorOnlySwallowsWhenItIsTheLastStage(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  string
		want bool
	}{
		// The live specimen.
		{"pipe then two && commands", `cat /etc/environment | head -1 && echo "PATH=x" > /tmp/p && cat /tmp/p`, false},
		{"pipe then one && command", "ls | head -3 && ./run-tests", false},
		{"pipe then a ; command", "ls | head -3; ./run-tests", false},
		{"pipe then a detach", "grep x f | head -5 & echo started", false},
		{"pipe then a newline command", "ls | head -3\n./run-tests", false},
		// Real masking: the truncator IS last.
		{"redirect before the pipe", "make world 2>&1 | tail -50", true},
		{"plain truncator", "make world | tail -20", true},
		// A redirect ON the truncator is part of that stage, not another command.
		{"redirect on the truncator", "make world | tail -50 2>&1", true},
		{"stderr to the truncator", "make world | head -20 2>&1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := swallowingPipe.MatchString(maskNonShell(c.cmd)); got != c.want {
				t.Errorf("swallowingPipe matched=%v, want %v for %q", got, c.want, c.cmd)
			}
			note := swallowingPipeNote(0, c.cmd, false)
			if (note != "") != c.want {
				t.Errorf("note fired=%v, want %v:\n%s", note != "", c.want, note)
			}
			if c.want && strings.ContainsAny(note, ";&") && !strings.Contains(note, "2>&1") {
				t.Errorf("the named stage must not carry a separator:\n%s", note)
			}
			// ExitCodeMasked reads the same predicate, so a phantom match would also tell
			// magi's own churn accounting to discard a real command's exit 0.
			if !c.want && ExitCodeMasked(c.cmd) {
				t.Errorf("a command whose last stage is real work is not masked: %q", c.cmd)
			}
		})
	}
}
