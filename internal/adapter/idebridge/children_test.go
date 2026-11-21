package idebridge

import (
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A call that ran a child names it at the end of its result; the row carries it so an editor can
// open the child from the call's own row, failed or not.
func TestAToolRowNamesTheChildItRan(t *testing.T) {
	rows := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c1", "name": "dummy_plug", "args": map[string]any{"goal": "count"}}}}, nil),
		mk(2, "part.appended", map[string]any{"messageId": "m1", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c1", "content": "35 docs\n\n[child s_kid01, 2 tool call(s), stopped short: boom]", "isError": true}}}, nil),
		mk(3, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c2", "name": "read", "args": map[string]any{"path": "a"}}}}, nil),
		mk(4, "part.appended", map[string]any{"messageId": "m1", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c2", "content": "hello", "isError": false}}}, nil),
	})
	var run, plain *Row
	for i := range rows {
		switch rows[i].CallID {
		case "c1":
			run = &rows[i]
		case "c2":
			plain = &rows[i]
		}
	}
	if run == nil || plain == nil {
		t.Fatalf("rows: %+v", rows)
	}
	if !reflect.DeepEqual(run.Children, []string{"s_kid01"}) {
		t.Fatalf("the row must name the child its call ran: %v", run.Children)
	}
	if plain.Children != nil {
		t.Fatalf("a call that ran no child names none: %v", plain.Children)
	}
}
