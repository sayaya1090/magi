package tui

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func resurfacePrompt(text, resurfacedFrom string) event.Event {
	data, _ := json.Marshal(event.PromptSubmittedData{
		Parts:          []session.Part{{Kind: session.PartText, Text: text}},
		ResurfacedFrom: resurfacedFrom,
	})
	return event.Event{
		Type:  event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		Data:  data,
	}
}

// A queued interjection's original bubble stays at its input position while queued;
// when it re-surfaces (ResurfacedFrom set) its bubble must be pulled to the end so the
// answer that follows renders right below it (Q&A pairing).
func TestApplyEvent_ResurfacedPromptReorders(t *testing.T) {
	m := &Model{blocks: []block{
		{kind: blockUser, text: "what's the capital of France?"}, // queued, shown up top
		{kind: blockToolCall, name: "bash", args: "{}"},
		{kind: blockAssistant, text: "…working on the original task…"},
	}}

	m.applyEvent(resurfacePrompt("what's the capital of France?", "m_orig"))

	if len(m.blocks) != 3 {
		t.Fatalf("reorder must not add/drop blocks: got %d", len(m.blocks))
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockUser || last.text != "what's the capital of France?" {
		t.Fatalf("query not moved to the end: %+v", last)
	}
	// The earlier position no longer holds a user block.
	if m.blocks[0].kind == blockUser {
		t.Errorf("stranded original still at its input position: %+v", m.blocks[0])
	}
}

// An ordinary user prompt (no ResurfacedFrom) is ignored by applyEvent — it was
// already echoed locally, so it must not double up.
func TestApplyEvent_OrdinaryUserPromptIgnored(t *testing.T) {
	m := &Model{blocks: []block{{kind: blockUser, text: "hi"}}}
	m.applyEvent(resurfacePrompt("hi", "")) // ResurfacedFrom empty → ordinary
	if len(m.blocks) != 1 {
		t.Fatalf("ordinary user prompt must not add a block: got %d", len(m.blocks))
	}
}

// No matching bubble (e.g. resumed session where local echo is absent): the query is
// appended as a fallback so it still pairs with its answer.
func TestApplyEvent_ResurfacedNoMatchAppends(t *testing.T) {
	m := &Model{blocks: []block{{kind: blockAssistant, text: "prior"}}}
	m.applyEvent(resurfacePrompt("brand new question", "m_orig"))
	if len(m.blocks) != 2 || m.blocks[1].kind != blockUser || m.blocks[1].text != "brand new question" {
		t.Fatalf("fallback append missing: %+v", m.blocks)
	}
}
