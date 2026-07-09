package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRouteInterjectionExecute(t *testing.T) {
	ctx := context.Background()

	// No RouteInterjection callback (a subagent) → refused.
	if r, _ := (RouteInterjection{}).Execute(ctx, json.RawMessage(`{"action":"queue","reason":"r"}`), port.ToolEnv{}); !r.IsError {
		t.Error("route_interjection without env.RouteInterjection should error (orchestrator-only)")
	}

	var gotAction, gotReason, gotReqID string
	env := port.ToolEnv{RouteInterjection: func(action, reason, requestID string) error {
		gotAction, gotReason, gotReqID = action, reason, requestID
		return nil
	}}

	// Unknown action → error, callback not invoked.
	gotAction = ""
	if r, _ := (RouteInterjection{}).Execute(ctx, json.RawMessage(`{"action":"nope","reason":"r"}`), env); !r.IsError {
		t.Error("an unknown action should error")
	}
	if gotAction != "" {
		t.Errorf("callback should not run on an invalid action, got %q", gotAction)
	}

	// Empty action defaults to the safe queue.
	gotAction = ""
	if r, _ := (RouteInterjection{}).Execute(ctx, json.RawMessage(`{"reason":"r"}`), env); r.IsError {
		t.Errorf("empty action should default to queue, got error: %q", resultText(t, r))
	}
	if gotAction != "queue" {
		t.Errorf("empty action should normalize to queue, got %q", gotAction)
	}

	// Each valid action (case-insensitive) routes through, trimmed reason preserved.
	for _, tc := range []struct{ in, want string }{
		{`{"action":"QUEUE","reason":" defer "}`, "queue"},
		{`{"action":"redirect","reason":"switch now"}`, "redirect"},
		{`{"action":"append","reason":"fold in"}`, "append"},
	} {
		gotAction, gotReason = "", ""
		r, _ := (RouteInterjection{}).Execute(ctx, json.RawMessage(tc.in), env)
		if r.IsError {
			t.Errorf("%s should succeed, got error: %q", tc.in, resultText(t, r))
		}
		if gotAction != tc.want {
			t.Errorf("%s → action %q, want %q", tc.in, gotAction, tc.want)
		}
	}
	if gotReason != "fold in" {
		t.Errorf("reason should be trimmed and forwarded, got %q", gotReason)
	}

	// request_id is optional, trimmed, and forwarded so the drain can target a specific request.
	gotReqID = ""
	if r, _ := (RouteInterjection{}).Execute(ctx, json.RawMessage(`{"action":"append","reason":"r","request_id":" ab12cd34 "}`), env); r.IsError {
		t.Errorf("request_id should be accepted, got error: %q", resultText(t, r))
	}
	if gotReqID != "ab12cd34" {
		t.Errorf("request_id should be trimmed and forwarded, got %q", gotReqID)
	}
}
