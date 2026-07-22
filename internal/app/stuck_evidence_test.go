package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// stuckEvidence must surface the CONCRETE obstacles (errored results, timeouts, missing files) so a
// stuck-recovery reason names the real wall — not the clean successes, and not a generic label.
func TestStuckEvidence(t *testing.T) {
	evs := []event.Event{
		evPrompt(),
		evToolCall("c1", "bash"), evToolResult("c1", "exit 0\ndrwxr-xr-x runtime", false), // clean → excluded
		evToolCall("c2", "bash"), evToolResult("c2", "cat: /app/ocaml/runtime/sweep.c: No such file or directory", false),
		evToolCall("c3", "bash"), evToolResult("c3", "make output...\n[timed out after 300s]", false),
		evToolCall("c4", "read"), evToolResult("c4", "some file contents", false), // clean → excluded
	}
	got := stuckEvidence(evs, 4)
	if got == "" {
		t.Fatal("stuckEvidence must return the obstacles, got empty")
	}
	if !strings.Contains(got, "No such file") || !strings.Contains(got, "timed out after 300s") {
		t.Errorf("must name the concrete walls (missing file, timeout), got: %s", got)
	}
	if strings.Contains(got, "drwxr-xr-x runtime") || strings.Contains(got, "some file contents") {
		t.Errorf("must NOT include clean successes, got: %s", got)
	}
	if !strings.Contains(got, "address THESE") {
		t.Errorf("must frame the obstacles as actionable, got: %s", got)
	}
}

// A turn with only clean successes yields no obstacle evidence (nothing to escalate).
func TestStuckEvidenceCleanTurn(t *testing.T) {
	evs := []event.Event{
		evPrompt(),
		evToolCall("c1", "bash"), evToolResult("c1", "exit 0\nok", false),
		evToolCall("c2", "write"), evToolResult("c2", "wrote 40 bytes", false),
	}
	if got := stuckEvidence(evs, 4); got != "" {
		t.Errorf("a clean turn must produce no obstacle evidence, got: %s", got)
	}
}
