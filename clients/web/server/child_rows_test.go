package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A call that ran a child carries it on its row, so the page can open the child from there. Read from
// the whole result: Out is clipped to its head and tail, and the child line must not depend on that.
func TestAToolRowCarriesTheChildItRan(t *testing.T) {
	long := strings.Repeat("x", 20000)
	content, _ := json.Marshal("35 docs\n" + long + "\n\n[child s_kid01, 2 tool call(s)]\n[child s_kid02, 3 step(s)] it reported:\nok")
	rows := renderMessages([]session.Message{{
		Role: session.RoleAssistant,
		Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "dummy_plug", Args: json.RawMessage(`{"goal":"count"}`)}},
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "c1", Content: content}},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c2", Name: "read", Args: json.RawMessage(`{"path":"a"}`)}},
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "c2", Content: json.RawMessage(`"hello"`)}},
		},
	}})
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	if !reflect.DeepEqual(rows[0].Children, []string{"s_kid01", "s_kid02"}) {
		t.Fatalf("the row must carry the children its call ran: %v", rows[0].Children)
	}
	if rows[1].Children != nil {
		t.Fatalf("a call that ran no child carries none: %v", rows[1].Children)
	}
	b, _ := json.Marshal(rows[1])
	if strings.Contains(string(b), "children") {
		t.Fatalf("an empty list is left off the wire: %s", b)
	}
}
