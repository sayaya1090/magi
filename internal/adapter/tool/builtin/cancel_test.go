package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// cancel_dispatch is orchestrator-only plumbing: without the injected hook it
// refuses; with it, it requires a reason, relays agent+reason, and phrases the
// three outcomes (nothing running / cancelled n / error) distinctly.
func TestCancelDispatchExecute(t *testing.T) {
	// No hook → not available to this agent.
	r, _ := CancelDispatch{}.Execute(context.Background(),
		json.RawMessage(`{"reason":"done early"}`), port.ToolEnv{})
	if !r.IsError || !strings.Contains(resultText(t, r), "orchestrator") {
		t.Fatalf("without the hook the tool must refuse: %s", resultText(t, r))
	}

	// Missing reason → rejected before the hook runs.
	called := false
	env := port.ToolEnv{CancelDispatch: func(agent, reason string) (int, error) {
		called = true
		return 0, nil
	}}
	r, _ = CancelDispatch{}.Execute(context.Background(), json.RawMessage(`{"reason":"  "}`), env)
	if !r.IsError || called {
		t.Fatalf("a blank reason must be rejected without calling the hook: %s", resultText(t, r))
	}

	// Nothing running → ok result that says so.
	r, _ = CancelDispatch{}.Execute(context.Background(), json.RawMessage(`{"reason":"answer found"}`), env)
	if r.IsError || !strings.Contains(resultText(t, r), "No running background subagents") {
		t.Fatalf("zero cancellations should read as a no-op: %s", resultText(t, r))
	}

	// Cancelled n → relays agent+reason and tells the model to review/compensate.
	var gotAgent, gotReason string
	env.CancelDispatch = func(agent, reason string) (int, error) {
		gotAgent, gotReason = agent, reason
		return 2, nil
	}
	r, _ = CancelDispatch{}.Execute(context.Background(),
		json.RawMessage(`{"agent":"explorer","reason":"answer found"}`), env)
	out := resultText(t, r)
	if r.IsError || !strings.Contains(out, "Cancelled 2") || !strings.Contains(out, "undo") {
		t.Fatalf("cancel result should count and warn about side effects: %s", out)
	}
	if gotAgent != "explorer" || gotReason != "answer found" {
		t.Errorf("hook got (%q,%q), want trimmed agent+reason", gotAgent, gotReason)
	}

	// Hook error → surfaced as the tool error.
	env.CancelDispatch = func(agent, reason string) (int, error) { return 0, errors.New("boom") }
	r, _ = CancelDispatch{}.Execute(context.Background(), json.RawMessage(`{"reason":"r"}`), env)
	if !r.IsError || !strings.Contains(resultText(t, r), "boom") {
		t.Fatalf("hook errors must surface: %s", resultText(t, r))
	}
}
