package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The turn's steps: since the last user prompt only, the unanswered (in-flight) call left out,
// a failed result's text carried.
func TestTurnStepsOfScopesToTheTurnAndDropsTheCallInFlight(t *testing.T) {
	call := func(id, name string) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(`{"slide":1}`)}}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	result := func(id, text string, isErr bool) event.Event {
		c, _ := json.Marshal(text)
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleTool, Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr}}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	prompt := event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser}}
	evs := []event.Event{
		call("a", "add_slides"), result("a", "made 3", false),
		prompt,
		call("b", "render_slide"), result("b", "no such slide", true),
		call("c", "set_text"), result("c", "ok", false),
		call("d", "land"), // in flight: no result yet
	}
	got := turnStepsOf(evs)
	if len(got) != 2 || got[0].Name != "render_slide" || got[1].Name != "set_text" {
		t.Fatalf("want the two answered calls of this turn, got %+v", got)
	}
	if !got[0].Failed || got[0].Output != "no such slide" || got[0].OutputBytes != len("no such slide") {
		t.Errorf("a failed step must carry its output, got %+v", got[0])
	}
	if got[1].Failed || got[1].Output != "" {
		t.Errorf("an ok step carries no output text, got %+v", got[1])
	}
	if turnStepsOf(nil) != nil {
		t.Error("no events → no steps")
	}
}
