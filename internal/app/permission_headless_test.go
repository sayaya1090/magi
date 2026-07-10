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

// In NON-interactive (headless/automation) mode there is no human to answer a
// permission prompt, so requestPermission must NEVER block — it resolves by policy.
// This is the fix for the deadlock where a guardrail-forced prompt (e.g. `rm -rf`)
// waited forever on a decision that could not come, sleeping every goroutine and
// crashing the process. A 2s deadline turns a regression (a block) into a failure.
func TestRequestPermissionNonInteractiveNeverBlocks(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"rm -rf /tmp/does-not-exist"}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	cases := []struct {
		perm  string
		force bool
		want  bool
	}{
		{"allow", true, true},  // forced prompt + headless allow → allow (allow = allow-all)
		{"allow", false, true}, // fast path
		{"ask", true, false},   // no human → safe deny
		{"ask", false, false},  // no human → safe deny
		{"auto", true, false},  // forced (risky) + headless → deny
		{"deny", true, false},  // deny always denies
	}
	for _, c := range cases {
		a, wd := newApp(t, &fakeLLM{}, Config{Permission: c.perm, Interactive: false})
		sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
		got := make(chan bool, 1)
		go func() { got <- a.requestPermission(context.Background(), sid, actor, tc, c.force, "") }()
		select {
		case g := <-got:
			if g != c.want {
				t.Errorf("perm=%q force=%v → %v, want %v", c.perm, c.force, g, c.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("perm=%q force=%v BLOCKED — headless must never wait on a prompt", c.perm, c.force)
		}
	}
}

// A headless denial is reported to the agent as a categorical "unavailable, don't
// retry" — not the misleading "denied by user" (there is no user), which would
// invite the agent to re-issue the same doomed call and flail. Interactive mode
// still says "denied by user" because a human really did decide.
func TestDenyReasonHeadlessVsInteractive(t *testing.T) {
	headless, _ := newApp(t, &fakeLLM{}, Config{Permission: "auto", Interactive: false})
	got := headless.denyReason("bash")
	for _, want := range []string{"headless", "Do not retry", "--permission allow", "bash"} {
		if !strings.Contains(got, want) {
			t.Errorf("headless denyReason %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "denied by user") {
		t.Errorf("headless denyReason must not claim a user decided: %q", got)
	}

	interactive, _ := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	if r := interactive.denyReason("bash"); r != "denied by user" {
		t.Errorf("interactive denyReason = %q, want \"denied by user\"", r)
	}
}

// Interactive mode is unchanged: a prompt still blocks until a human answers it via
// RespondPermission (the fix must not silently auto-resolve when a human IS present).
func TestRequestPermissionInteractiveStillPrompts(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	tc := &session.ToolCall{CallID: "c2", Name: "bash", Args: json.RawMessage(`{"command":"echo hi"}`)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan bool, 1)
	go func() {
		got <- a.requestPermission(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "u"}, tc, false, "")
	}()

	// It must BLOCK waiting for a human, not resolve immediately.
	select {
	case <-got:
		t.Fatal("interactive ask should block until answered, not auto-resolve")
	case <-time.After(200 * time.Millisecond):
	}

	if err := a.RespondPermission(context.Background(), command.RespondPermission{SessionID: sid, CallID: "c2", Decision: "allow"}); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	select {
	case g := <-got:
		if !g {
			t.Error("answered 'allow' → want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no resolution after RespondPermission")
	}
}
