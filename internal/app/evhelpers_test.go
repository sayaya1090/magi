package app

import (
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Event constructors shared by the probe tests. They lived in council_lookup_test.go until the
// detector that file covered was removed; the probes that still assert live behaviour need them.

func evPrompt() event.Event {
	return event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}
}

func evToolCall(callID, name string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind:     session.PartToolCall,
		ToolCall: &session.ToolCall{CallID: callID, Name: name},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func evToolResult(callID, content string, isErr bool) event.Event {
	c, _ := json.Marshal(content)
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind:       session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: callID, Content: c, IsError: isErr},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}
