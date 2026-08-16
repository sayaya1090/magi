package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// An MCP tool is a process this binary did not write, reached under a name the DangerTools map —
// filled at startup — cannot know. It used to run without a prompt in EVERY mode, deny included,
// while EXTENDING promised it went through the same permission gate as any other tool. The gate now
// reads danger off the `mcp__` namespace the manager enforces, so the promise is a mechanism.
func TestMCPToolsAreDangerGated(t *testing.T) {
	tc := &session.ToolCall{CallID: "m1", Name: "mcp__files__write_file",
		Args: json.RawMessage(`{"path":"x.txt","content":"hi"}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	cases := []struct {
		perm string
		stop bool
	}{
		{"ask", true},    // no human in this headless run → safe deny
		{"auto", true},   // not a file-modifier magi can vouch for → same as a command
		{"deny", true},   // deny denies
		{"allow", false}, // allow-all is the headless posture and still waves it through
	}
	for _, c := range cases {
		a, wd := newApp(t, &fakeLLM{}, Config{Permission: c.perm, Interactive: false})
		sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
		if stop := a.gatePermission(context.Background(), sid, actor, tc, "msg"); stop != c.stop {
			t.Errorf("perm=%q: gatePermission stop=%v, want %v", c.perm, stop, c.stop)
		}
	}
}

// An allow rule can still pre-approve one MCP tool the operator trusts, exactly as it pre-approves
// a program for bash — the gate is a default, not a wall.
func TestMCPToolAllowRuleSkipsThePrompt(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: false,
		Allow: []string{"mcp__files__read_file(**)"}})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	tc := &session.ToolCall{CallID: "m2", Name: "mcp__files__read_file",
		Args: json.RawMessage(`{"path":"x.txt"}`)}
	if stop := a.gatePermission(context.Background(), sid,
		event.Actor{Kind: event.ActorUser, ID: "u"}, tc, "msg"); stop {
		t.Fatal("an explicit allow rule for this MCP tool should skip the prompt")
	}
}

// Danger-gated means serialized too: an MCP call can prompt (one modal slot), and what it does on
// its server is nothing magi can prove read-only — so a batch containing one never runs in parallel.
func TestMCPToolsAreNotParallelSafe(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow", Interactive: false})
	calls := []*session.ToolCall{
		{CallID: "r1", Name: "read", Args: json.RawMessage(`{"path":"a"}`)},
		{CallID: "m3", Name: "mcp__files__list", Args: json.RawMessage(`{}`)},
	}
	if a.allParallelSafe(calls) {
		t.Fatal("a batch holding an MCP call must not be declared parallel-safe")
	}
}
