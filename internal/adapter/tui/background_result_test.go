package tui

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A background job's completion lands on the block of the call that launched it, live: the block
// shows what the job printed instead of "started", reads as failed when it failed, and names the
// child it ran so the row opens it.
func TestABackgroundCompletionFillsItsCallsBlock(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.applyEvent(ev(t, event.TypePartAppended, event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
		Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "job", Name: "dummy_plug", Args: json.RawMessage(`{}`)}}}))
	m.applyEvent(ev(t, event.TypePartAppended, event.PartAppendedData{Role: session.RoleTool, Part: session.Part{
		Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "job", Background: true, Content: json.RawMessage(`"Started in the background."`)}}}))
	detail := "exit 1\nboom\n\n[child s_bgchild01, 7 tool call(s)]"
	m.applyEvent(ev(t, event.TypeToolBackground, map[string]any{"callId": "job", "state": "failed", "exit": 1, "detail": detail}))
	var b *block
	for i := range m.blocks {
		if m.blocks[i].kind == blockToolCall && m.blocks[i].callID == "job" {
			b = &m.blocks[i]
		}
	}
	if b == nil {
		t.Fatal("no block for the call")
	}
	if b.result != detail {
		t.Fatalf("the block shows its launch, not its result: %q", b.result)
	}
	if b.ok {
		t.Fatal("a failed job reads as a success")
	}
	if len(b.children) != 1 || b.children[0] != "s_bgchild01" {
		t.Fatalf("the block cannot open the child it ran: %v", b.children)
	}
}
