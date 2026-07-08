package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestReplanExecute(t *testing.T) {
	ctx := context.Background()

	// No Replan callback (read-only / max-depth agent) → refused.
	if r, _ := (Replan{}).Execute(ctx, json.RawMessage(`{"reason":"dead end"}`), port.ToolEnv{}); !r.IsError {
		t.Error("replan without env.Replan should error (plan-eligible only)")
	}

	var gotReason string
	env := port.ToolEnv{Replan: func(reason string) error { gotReason = reason; return nil }}

	// Missing/blank reason → error, callback not invoked.
	gotReason = ""
	if r, _ := (Replan{}).Execute(ctx, json.RawMessage(`{"reason":"  "}`), env); !r.IsError {
		t.Error("replan without a reason should error")
	}
	if gotReason != "" {
		t.Errorf("callback should not run on a blank reason, got %q", gotReason)
	}

	// Valid: trimmed reason forwarded to the callback.
	r, _ := (Replan{}).Execute(ctx, json.RawMessage(`{"reason":"  the target API doesn't exist  "}`), env)
	if r.IsError {
		t.Fatalf("valid replan should succeed, got error: %q", resultText(t, r))
	}
	if gotReason != "the target API doesn't exist" {
		t.Errorf("reason should be trimmed and forwarded, got %q", gotReason)
	}
}
