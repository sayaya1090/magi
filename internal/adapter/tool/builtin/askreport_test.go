package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestAskExecute(t *testing.T) {
	ctx := context.Background()
	// No Ask callback (top-level agent) → error.
	if r, _ := (Ask{}).Execute(ctx, json.RawMessage(`{"question":"q"}`), port.ToolEnv{}); !r.IsError {
		t.Error("ask without env.Ask should error")
	}
	env := port.ToolEnv{Ask: func(q string) (string, error) { return "ANSWER:" + q, nil }}
	// Missing question → error.
	if r, _ := (Ask{}).Execute(ctx, json.RawMessage(`{}`), env); !r.IsError {
		t.Error("ask without a question should error")
	}
	// Normal: the callback's answer is returned.
	r, _ := (Ask{}).Execute(ctx, json.RawMessage(`{"question":"hi"}`), env)
	if r.IsError || !strings.Contains(resultText(t, r), "ANSWER:hi") {
		t.Errorf("ask result = %q", resultText(t, r))
	}
}

func TestReportExecute(t *testing.T) {
	ctx := context.Background()
	// No Report callback → error.
	if r, _ := (Report{}).Execute(ctx, json.RawMessage(`{"summary":"s"}`), port.ToolEnv{}); !r.IsError {
		t.Error("report without env.Report should error")
	}
	var gotStatus string
	env := port.ToolEnv{Report: func(summary, status, details string) error { gotStatus = status; return nil }}
	// A known status passes through.
	if r, _ := (Report{}).Execute(ctx, json.RawMessage(`{"summary":"s","status":"blocked"}`), env); r.IsError || gotStatus != "blocked" {
		t.Errorf("blocked status = %q (err=%v)", gotStatus, r.IsError)
	}
	// An unknown/empty status normalizes to "done".
	(Report{}).Execute(ctx, json.RawMessage(`{"summary":"s","status":"weird"}`), env)
	if gotStatus != "done" {
		t.Errorf("unknown status should normalize to done, got %q", gotStatus)
	}
	(Report{}).Execute(ctx, json.RawMessage(`{"summary":"s"}`), env)
	if gotStatus != "done" {
		t.Errorf("empty status should default to done, got %q", gotStatus)
	}
}

// A "done" report whose text confesses the work is a stand-in is refused inline and never
// filed; the same admission under "failed" is honest and passes through.
func TestReportRefusesFabricatedDone(t *testing.T) {
	ctx := context.Background()
	filed := false
	env := port.ToolEnv{Report: func(summary, status, details string) error { filed = true; return nil }}

	r, _ := (Report{}).Execute(ctx, json.RawMessage(`{"status":"done","details":"In a real implementation this would run the program."}`), env)
	if !r.IsError {
		t.Error("fabricated done report should be refused")
	}
	if filed {
		t.Error("fabricated done report must not be filed via env.Report")
	}
	if !strings.Contains(resultText(t, r), "stand-in") {
		t.Errorf("refusal should explain the problem, got %q", resultText(t, r))
	}

	// Honest failure carrying the same phrase is allowed through.
	filed = false
	if r, _ := (Report{}).Execute(ctx, json.RawMessage(`{"status":"failed","summary":"in a real implementation this would run"}`), env); r.IsError || !filed {
		t.Errorf("failed report should pass through (err=%v filed=%v)", r.IsError, filed)
	}
}
