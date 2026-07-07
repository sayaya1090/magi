package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestResolveConcernExecute(t *testing.T) {
	ctx := context.Background()

	// No ResolveConcern callback (a subagent, or any non-orchestrator) → refused.
	if r, _ := (ResolveConcern{}).Execute(ctx, json.RawMessage(`{"key":"k","reason":"r"}`), port.ToolEnv{}); !r.IsError {
		t.Error("resolveconcern without env.ResolveConcern should error (orchestrator-only)")
	}

	var gotKey, gotReason string
	env := port.ToolEnv{ResolveConcern: func(key, reason string) error { gotKey, gotReason = key, reason; return nil }}

	// Missing key → error, callback not invoked.
	if r, _ := (ResolveConcern{}).Execute(ctx, json.RawMessage(`{"reason":"r"}`), env); !r.IsError {
		t.Error("resolveconcern without a key should error")
	}
	if gotKey != "" {
		t.Errorf("callback should not run on a missing key, got %q", gotKey)
	}

	// Normal: key+reason routed to the callback, success text returned.
	r, _ := (ResolveConcern{}).Execute(ctx, json.RawMessage(`{"key":"self-check/unverified-premise","reason":"verified via websearch"}`), env)
	if r.IsError {
		t.Fatalf("valid resolveconcern should succeed, got error: %q", resultText(t, r))
	}
	if gotKey != "self-check/unverified-premise" || gotReason != "verified via websearch" {
		t.Errorf("callback args = (%q, %q)", gotKey, gotReason)
	}
	if !strings.Contains(resultText(t, r), "re-raised") {
		t.Errorf("result should warn it is advisory (re-raised next turn), got %q", resultText(t, r))
	}
}
