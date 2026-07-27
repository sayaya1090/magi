package app

import (
	"strings"
	"testing"
)

// The loop-guard block message steers two fixation loops toward the right alternative:
// a read loop off another read, and a bash_output poll loop toward wait_for + independent
// work (the compile-compcert stall). Every other tool gets the generic "different step".
func TestLoopGuardBlockMsg(t *testing.T) {
	full := AgentSpec{Name: "worker"} // nil Tools == allowed everything

	// bash_output: the compcert fix — must name wait_for and independent work, not the generic.
	bo := loopGuardBlockMsg(full, "bash_output", 4)
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
	rd := loopGuardBlockMsg(full, "read", 3)
	if !strings.Contains(rd, "Do NOT read it again") || !strings.Contains(rd, "wait_for") {
		t.Errorf("read block lost its steer:\n%s", rd)
	}

	// Any other tool → generic, and it names the tool.
	other := loopGuardBlockMsg(full, "bash", 3)
	if !strings.Contains(other, "take a different step") || !strings.Contains(other, `"bash"`) {
		t.Errorf("generic block message wrong for a non-fixation tool:\n%s", other)
	}
}

// A nudge fires when the agent is ALREADY stuck, which is the worst moment to point it at a
// tool its allowlist refuses — the redirect becomes a refused call and it is stuck twice. The
// default read-only agents hold no wait_for, and the curated worker's set has bash_output
// without it, so each steer has to be built from what THIS agent can reach.
func TestNudgesNameOnlyWaitToolsTheAgentHolds(t *testing.T) {
	readOnly := AgentSpec{Name: "explore", Tools: []string{"read", "grep", "glob", "list", "report"}}
	noBlock := AgentSpec{Name: "worker", Tools: []string{"read", "write", "bash", "bash_output", "report"}}
	full := AgentSpec{Name: "solo"}

	// The steer itself: each agent is offered exactly the waiting it has.
	for _, c := range []struct{ name, got, want string }{
		{"read-only", waitSteer(readOnly), ""},
		{"bash but no wait_for", waitSteer(noBlock), "poll bash_output"},
	} {
		if c.got != c.want {
			t.Errorf("%s waitSteer = %q, want %q", c.name, c.got, c.want)
		}
	}
	if s := waitSteer(full); !strings.Contains(s, "wait_for") || !strings.Contains(s, "bash_output") {
		t.Errorf("an unrestricted agent should be offered both ways to wait, got %q", s)
	}

	// The stall nudge's background clause: a read-only agent cannot start, poll, OR block on a
	// background job, so the clause is dropped whole rather than trimmed to a tool it lacks.
	if adv := backgroundWaitAdvice(readOnly); adv != "" {
		t.Errorf("a read-only agent must not be told to run a background job: %q", adv)
	}
	if adv := backgroundWaitAdvice(noBlock); !strings.Contains(adv, "bash_output") || strings.Contains(adv, "wait_for") {
		t.Errorf("advice must name bash_output and NOT wait_for for this agent: %q", adv)
	}

	// The loop-guard steers drop their wait_for sentence but keep the rest of the correction —
	// the point of the nudge is to stop the repeat, and that part is true for every agent.
	rd := loopGuardBlockMsg(readOnly, "read", 3)
	if strings.Contains(rd, "wait_for") || !strings.Contains(rd, "Do NOT read it again") {
		t.Errorf("read steer must lose only the wait_for sentence:\n%s", rd)
	}
	bo := loopGuardBlockMsg(noBlock, "bash_output", 4)
	if strings.Contains(bo, "wait_for") || !strings.Contains(bo, "depend on this job") {
		t.Errorf("bash_output steer must lose only the wait_for sentence:\n%s", bo)
	}
}
