package app

import (
	"strings"
	"testing"
)

// The loop-guard block message steers two fixation loops toward the right alternative:
// a read loop off another read, and a bash_output poll loop toward wait_for + independent
// work (the compile-compcert stall). Every other tool gets the generic "different step".
func TestLoopGuardBlockMsg(t *testing.T) {
	// bash_output: the compcert fix — must name wait_for and independent work, not the generic.
	bo := loopGuardBlockMsg("bash_output", 4)
	if !strings.Contains(bo, "wait_for") {
		t.Errorf("bash_output block must steer to wait_for:\n%s", bo)
	}
	if !strings.Contains(bo, "does NOT depend on this job") && !strings.Contains(bo, "not depend on this job") {
		t.Errorf("bash_output block must steer to independent work:\n%s", bo)
	}
	if strings.Contains(bo, "take a different step") {
		t.Errorf("bash_output must NOT fall back to the generic message:\n%s", bo)
	}

	// read: keeps its own steer (off another read, wait_for on a change).
	rd := loopGuardBlockMsg("read", 3)
	if !strings.Contains(rd, "Do NOT read it again") || !strings.Contains(rd, "wait_for") {
		t.Errorf("read block lost its steer:\n%s", rd)
	}

	// Any other tool → generic, and it names the tool.
	other := loopGuardBlockMsg("bash", 3)
	if !strings.Contains(other, "take a different step") || !strings.Contains(other, `"bash"`) {
		t.Errorf("generic block message wrong for a non-fixation tool:\n%s", other)
	}
}
