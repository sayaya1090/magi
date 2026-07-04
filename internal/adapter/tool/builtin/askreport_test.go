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

// A fabricated "done" is no longer caught by scanning the report's prose for English
// confession phrases (that missed other languages and non-confessing fakes). The report tool
// files any well-formed status; the behavioral catch lives in the loop's take-report branch
// (runGuard.unverifiedDeliverable) and the parent's review-gate tester — see report.go and
// internal/app.TestSubagentFabricatedDoneRefused.
