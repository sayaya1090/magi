package app

import (
	"strings"
	"testing"
)

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
}
