package tui

import (
	"strings"
	"testing"
)

// A `!` run is the one place the user hands the agent something the agent did not fetch. Everything
// about that hand-off fails quietly if it fails: the transcript shows the output either way, so a
// user who ran `!ls` and then asked a question reasonably assumes the agent can see it.

// Staged output reaches the NEXT prompt, and only once. Never reaching it means the agent answers
// about a workspace it cannot see; reaching it twice means an old command's output is re-sent as
// context for an unrelated question.
func TestAStagedShellRunReachesTheNextPromptExactlyOnce(t *testing.T) {
	s := newScript(t)
	s.m.applyShellResult(shellResultMsg{cmd: "ls -1", out: "alpha.txt\nbeta.txt\n", exit: 0})

	if len(s.m.pendingShell) != 1 {
		t.Fatalf("the run was not staged: %+v", s.m.pendingShell)
	}
	pre := s.m.drainPendingShell()
	for _, want := range []string{"ls -1", "alpha.txt", "beta.txt", "(exit 0)"} {
		if !strings.Contains(pre, want) {
			t.Errorf("the preamble does not carry %q:\n%s", want, pre)
		}
	}
	if again := s.m.drainPendingShell(); again != "" {
		t.Errorf("a second prompt got the same shell output again:\n%s", again)
	}
}

// A failing command must carry its exit into the preamble and render as failed. The agent reading
// "(exit 0)" under a command that failed is the display half of the false-success problem.
func TestAFailedShellRunSaysSoBothOnScreenAndInTheContext(t *testing.T) {
	s := newScript(t)
	s.m.applyShellResult(shellResultMsg{cmd: "false", out: "", exit: 3})

	if !strings.Contains(s.m.drainPendingShell(), "(exit 3)") {
		t.Error("the exit did not reach the agent's context")
	}
	last := s.m.blocks[len(s.m.blocks)-1]
	if last.kind != blockShell {
		t.Fatalf("the run left no transcript block, got kind %v", last.kind)
	}
	if last.ok {
		t.Error("a command that exited 3 renders as one that worked")
	}
	if !strings.Contains(last.result, "exit 3") {
		t.Errorf("the block does not show the exit: %q", last.result)
	}
}

// Output past the cap is cut, and the cut is MARKED — the same rule the tool results and the
// evidence block follow. An unmarked cut reads as the whole output.
func TestOversizedShellOutputIsCutAndSaysSo(t *testing.T) {
	s := newScript(t)
	huge := strings.Repeat("x", maxShellOut*2)
	s.m.applyShellResult(shellResultMsg{cmd: "cat big", out: huge, exit: 0})

	last := s.m.blocks[len(s.m.blocks)-1]
	if len(last.text) >= len(huge) {
		t.Fatalf("nothing was cut from %d bytes", len(huge))
	}
	if !strings.Contains(last.text, "truncated") {
		t.Errorf("an unmarked cut reads as the whole output:\n…%s", last.text[max(0, len(last.text)-80):])
	}
	// …and the agent gets the cut copy, not the original.
	if pre := s.m.drainPendingShell(); len(pre) > maxShellOut+512 {
		t.Errorf("the preamble carries %d bytes past a %d cap", len(pre), maxShellOut)
	}
}

// While a turn is running the output is steered into it instead of staged, so it is not left
// waiting for a prompt that may never come. Staging it in that case is how it would go unseen.
func TestAShellRunDuringATurnIsSteeredNotStaged(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "keep working")
	s.m.running = true

	s.m.applyShellResult(shellResultMsg{cmd: "pwd", out: "/app\n", exit: 0})

	if len(s.m.pendingShell) != 0 {
		t.Errorf("mid-turn output was staged for later instead of steered: %+v", s.m.pendingShell)
	}
	if !strings.Contains(s.m.snackbar, "steered") {
		t.Errorf("the user was not told where the output went: %q", s.m.snackbar)
	}
	if last := s.m.blocks[len(s.m.blocks)-1]; last.kind != blockShell {
		t.Error("the run is missing from the transcript")
	}
}

// Several staged runs arrive in the order they were run — a preamble that reorders them tells the
// agent a sequence of commands happened in an order it did not.
func TestStagedRunsKeepTheirOrder(t *testing.T) {
	s := newScript(t)
	for _, c := range []string{"first", "second", "third"} {
		s.m.applyShellResult(shellResultMsg{cmd: c, out: c + " ran\n", exit: 0})
	}
	pre := s.m.drainPendingShell()
	i, j, k := strings.Index(pre, "first"), strings.Index(pre, "second"), strings.Index(pre, "third")
	if !(i >= 0 && i < j && j < k) {
		t.Errorf("staged runs are out of order (%d, %d, %d):\n%s", i, j, k, pre)
	}
}
