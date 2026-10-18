package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestDelegatedApprovalStaysPendingUntilExplicitResponse(t *testing.T) {
	a, _, sid := handedSession(t, Config{Permission: "allow", Interactive: true, AnswerWait: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan permissionOutcome, 1)
	go func() {
		var out permissionOutcome
		a.requestPermission(ctx, sid, event.Actor{}, &session.ToolCall{CallID: "pending", Name: "bash"}, false, "", &out)
		done <- out
	}()
	deadline := time.After(2 * time.Second)
	for len(a.PendingPrompts()) == 0 {
		select {
		case <-deadline:
			t.Fatal("approval never exposed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case out := <-done:
		t.Fatalf("delegated approval expired: %+v", out)
	case <-time.After(50 * time.Millisecond):
	}
	prompts := a.PendingPrompts()
	if len(prompts) != 1 || prompts[0].Session != sid || prompts[0].Ask.ID != "pending" {
		t.Fatalf("wrong approval route: %+v", prompts)
	}
	if err := a.RespondPermission(ctx, command.RespondPermission{SessionID: sid, CallID: "pending", Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.Origin != "user" || out.Reason != "denied by user" {
			t.Fatalf("wrong rejection: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("response not delivered")
	}
	if len(a.PendingPrompts()) != 0 {
		t.Fatal("answered prompt remained pending")
	}
}

func TestPermissionOutcomeDistinguishesTimeoutCancellationAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, mode, origin  string
		interactive, cancel bool
	}{
		{"timeout", "auto", "timeout", true, false},
		{"cancel", "ask", "cancelled", true, true},
		{"policy", "deny", "policy", true, false},
		{"allow", "allow", "policy", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, wd := newApp(t, &fakeLLM{}, Config{Permission: tc.mode, Interactive: tc.interactive, AnswerWait: time.Millisecond})
			sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			var out permissionOutcome
			allowed := a.requestPermission(ctx, sid, event.Actor{}, &session.ToolCall{CallID: "c", Name: "bash"}, false, "", &out)
			if out.Origin != tc.origin {
				t.Fatalf("outcome %+v", out)
			}
			if allowed {
				if out.Reason != "" {
					t.Fatalf("allowed with denial reason: %+v", out)
				}
			} else if out.Reason == "" || strings.Contains(out.Reason, "denied by user") {
				t.Fatalf("misattributed refusal: %+v", out)
			}
		})
	}
}

func TestPermissionGateRecordsCancellationWithoutCallingItUserDenial(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !a.gatePermission(ctx, sid, event.Actor{}, &session.ToolCall{CallID: "cancelled", Name: "bash", Args: json.RawMessage(`{"command":"git status"}`)}, "tool") {
		t.Fatal("cancelled call allowed")
	}
	events, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Type == event.TypePermissionDecided {
			var outcome event.PermissionDecidedData
			if err := json.Unmarshal(e.Data, &outcome); err != nil {
				t.Fatal(err)
			}
			if outcome.Origin != "cancelled" || outcome.Decision != "deny" || strings.Contains(outcome.Reason, "denied by user") {
				t.Fatalf("wrong audit: %+v", outcome)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("cancellation was not recorded")
	}
}
