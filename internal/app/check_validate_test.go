package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// parseChecksArray must pull a JSON array of checks out of a review reply and drop any check with no
// command (nothing to run), and reject non-array replies.
func TestParseChecksArray(t *testing.T) {
	raw := "here you go:\n[{\"step\":\"1\",\"deliverable\":\"deps\",\"command\":\"grep -q x\"}," +
		"{\"step\":\"2\",\"command\":\"\"}]\nthanks"
	out, ok := parseChecksArray(raw)
	if !ok {
		t.Fatal("a reply containing a JSON array must parse")
	}
	if len(out) != 1 || out[0].Command != "grep -q x" {
		t.Fatalf("must keep the one runnable check and drop the empty-command one, got %+v", out)
	}
	if _, ok := parseChecksArray("no array here"); ok {
		t.Error("a reply with no JSON array must not parse")
	}
}

// With the flag off, validateChecks is a pure passthrough — the authored checks are used as-is and no
// review call is made (the input is returned unchanged, including its exact contents).
func TestValidateChecksFlagOffPassthrough(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "0")
	a := newOrchApp(t, &gateLLM{text: "[]"}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "pip show grpcio | grep -q Version"}}
	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Command != in[0].Command {
		t.Fatalf("flag off must return the authored checks unchanged, got %+v", out)
	}
}
