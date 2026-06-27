package openai

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// helpers to build domain messages compactly.
func asstCall(id, name string) session.Message {
	return session.Message{Role: session.RoleAssistant, Parts: []session.Part{
		{Kind: session.PartText, Text: "calling " + name},
		{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(`{}`)}},
	}}
}
func toolRes(id, content string) session.Message {
	b, _ := json.Marshal(content)
	return session.Message{Role: session.RoleTool, Parts: []session.Part{
		{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: b}},
	}}
}

// wireRoles converts and returns the role sequence for assertion.
func wireRoles(msgs []session.Message) []string {
	wm := convertMessages(msgs)
	roles := make([]string, len(wm))
	for i, m := range wm {
		roles[i] = m.Role
	}
	return roles
}

// A message injected between an assistant tool-call and its result (the async
// subagent-result race) must not separate the tool message from its assistant.
func TestRepairPullsToolResultAfterAssistant(t *testing.T) {
	in := []session.Message{
		userMsg("do it"),
		asstCall("c1", "task"),
		userMsg("[subagent x result] ..."), // injected between call and result
		toolRes("c1", "dispatched"),
	}
	got := wireRoles(in)
	want := []string{"user", "assistant", "tool", "user"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

// An orphaned tool result (assistant tool-call dropped by compaction) must be
// demoted to a user message, never emitted as a bare tool message.
func TestRepairDemotesOrphanToolResult(t *testing.T) {
	in := []session.Message{
		{Role: session.RoleSystem, Parts: []session.Part{{Kind: session.PartText, Text: "[compacted summary]"}}},
		toolRes("gone", "orphaned output"), // its assistant tool-call was summarized away
		userMsg("continue"),
	}
	wm := convertMessages(in)
	for _, m := range wm {
		if m.Role == "tool" {
			t.Fatalf("orphan tool result must be demoted, got a tool message: %+v", m)
		}
	}
	// content preserved in a user message
	found := false
	for _, m := range wm {
		if m.Role == "user" {
			if s, ok := m.Content.(string); ok && len(s) > 0 && contains(s, "orphaned output") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("orphan tool content should be preserved in a user message: %+v", wm)
	}
}

// An assistant tool-call with no result anywhere gets a synthetic placeholder so
// strict backends don't reject the dangling call.
func TestRepairFillsMissingResult(t *testing.T) {
	in := []session.Message{
		asstCall("c1", "task"),
		// no tool result for c1
	}
	got := wireRoles(in)
	want := []string{"assistant", "tool"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("roles = %v, want %v (placeholder result expected)", got, want)
	}
}

// The common chat-only path must be untouched.
func TestRepairNoToolsPassThrough(t *testing.T) {
	in := []session.Message{userMsg("hi"), {Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartText, Text: "hello"}}}}
	got := wireRoles(in)
	if len(got) != 2 || got[0] != "user" || got[1] != "assistant" {
		t.Fatalf("chat-only roles = %v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
