package tui

import (
	"strings"
	"testing"
	"time"
)

// A background job's status line is the only place the pane says how the job ended. Showing a
// failure as a success there is the display half of the problem the rest of magi works to prevent:
// the record says exit 3, the screen says it finished.
func TestAJobsStatusLineSaysHowItReallyEnded(t *testing.T) {
	for _, tc := range []struct {
		what string
		pane agentPane
		want string
		deny string
	}{
		{"still running", agentPane{started: time.Now().Add(-3 * time.Second)}, "running", "exited"},
		{"clean exit", agentPane{exited: true, exit: 0, dur: 2 * time.Second}, "exited 0", "killed"},
		{"failed", agentPane{exited: true, exit: 3, dur: time.Second}, "exited 3", "exited 0"},
		// A killed job reports the kill, not a code: the agent stopped it, so the status says
		// nothing about the work — and "exited 137" would read as the program's own verdict.
		{"killed", agentPane{killed: true, exited: true, exit: 137}, "killed", "137"},
	} {
		got := jobStatus(&tc.pane)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not say %q", tc.what, got, tc.want)
		}
		if strings.Contains(got, tc.deny) {
			t.Errorf("%s: %q wrongly says %q", tc.what, got, tc.deny)
		}
	}
}

// A killed job takes precedence over its exit code even when both are recorded, because the code
// belongs to the signal that stopped it rather than to the work.
func TestAKilledJobNeverReportsAnExitCode(t *testing.T) {
	for _, exit := range []int{0, 1, 137, -1} {
		p := agentPane{killed: true, exited: true, exit: exit, dur: time.Second}
		if got := jobStatus(&p); got != "killed" {
			t.Errorf("killed with exit %d reported %q", exit, got)
		}
	}
}

// Not tested here: syncJobPanes marking a finished job done exactly once (doneAt starts the fade,
// and re-marking on every poll would pin a finished pane on screen for the rest of the session).
// The transition is inline in syncJobPanes over the process-global job registry, and neither is
// injectable — reaching it would mean either running a real background process or splitting the
// function open to suit a test. The invariant is real and stated in the code's own comment; this
// note is here so the gap is a known one rather than an assumed pass.
