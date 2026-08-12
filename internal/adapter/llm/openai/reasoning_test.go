package openai

import "testing"

// The thinking budget is settable from the config, and the environment still outranks it.
//
// It was reachable only as MAGI_REASONING_EFFORT — a machine-wide switch for a per-companion
// choice: a planner that should think and a formatter that should not are two companions on one
// machine, and one variable cannot say both.
func TestReasoningEffortComesFromConfigAndEnvWins(t *testing.T) {
	c := New("http://x/v1", "", WithSampling(Sampling{ReasoningEffort: "high"}))
	if c.reasoningEffort != "high" {
		t.Errorf("config value did not reach the client: %q", c.reasoningEffort)
	}
	// Unset stays unset, so a non-thinking model is handed no field at all.
	if plain := New("http://x/v1", ""); plain.reasoningEffort != "" {
		t.Errorf("a client with nothing configured sends %q", plain.reasoningEffort)
	}
	t.Setenv("MAGI_REASONING_EFFORT", "none")
	if c := New("http://x/v1", "", WithSampling(Sampling{ReasoningEffort: "high"})); c.reasoningEffort != "none" {
		t.Errorf("the environment lost to the config file: %q", c.reasoningEffort)
	}
}
