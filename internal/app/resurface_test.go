package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// promptEvent builds a TypePromptSubmitted event with the given MessageID and text,
// optionally linked back to an original via ResurfacedFrom.
func promptEvent(seq int64, msgID, text, resurfacedFrom string) event.Event {
	data, _ := json.Marshal(event.PromptSubmittedData{
		MessageID:      msgID,
		Parts:          []session.Part{{Kind: session.PartText, Text: text}},
		ResurfacedFrom: resurfacedFrom,
	})
	return event.Event{Seq: seq, Type: event.TypePromptSubmitted, Data: data}
}

func assistantEvent(seq int64, msgID, text string) event.Event {
	data, _ := json.Marshal(event.PartAppendedData{
		MessageID: msgID,
		Role:      session.RoleAssistant,
		Part:      session.Part{Kind: session.PartText, Text: text},
	})
	return event.Event{Seq: seq, Type: event.TypePartAppended, Data: data}
}

// A queued interjection that was later re-surfaced (linked via ResurfacedFrom) must
// show ONCE, next to its answer — the stranded original prompt is dropped and the
// re-emitted copy sits at the back of the stream just before the assistant reply.
func TestDropResurfacedOrigins(t *testing.T) {
	evs := []event.Event{
		promptEvent(1, "m_orig", "what's the capital of France?", ""), // typed mid-turn, stranded up top
		assistantEvent(2, "a_task", "…working on the original task…"),
		promptEvent(3, "m_resurf", "what's the capital of France?", "m_orig"), // re-emitted at drain
		assistantEvent(4, "a_ans", "Paris."),
	}

	got := dropResurfacedOrigins(evs)

	// The stranded original (m_orig) is gone; the re-surfaced copy remains.
	for _, e := range got {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil && d.MessageID == "m_orig" {
			t.Fatalf("stranded original prompt m_orig was not dropped")
		}
	}

	msgs := reconstruct(got)
	// Exactly one user message survives, and it is immediately followed by its answer.
	var userIdx = -1
	userCount := 0
	for i, m := range msgs {
		if m.Role == session.RoleUser {
			userCount++
			userIdx = i
		}
	}
	if userCount != 1 {
		t.Fatalf("want exactly 1 user message after dedup, got %d", userCount)
	}
	if userIdx != len(msgs)-2 || msgs[len(msgs)-1].Role != session.RoleAssistant {
		t.Fatalf("re-surfaced query not paired with its answer: userIdx=%d of %d, last role=%v",
			userIdx, len(msgs), msgs[len(msgs)-1].Role)
	}
}

// Without any ResurfacedFrom links, the filter is a no-op (ordinary prompts untouched).
func TestDropResurfacedOriginsNoOp(t *testing.T) {
	evs := []event.Event{
		promptEvent(1, "m1", "hi", ""),
		assistantEvent(2, "a1", "hello"),
		promptEvent(3, "m2", "bye", ""),
	}
	got := dropResurfacedOrigins(evs)
	if len(got) != len(evs) {
		t.Fatalf("no-op filter changed event count: %d -> %d", len(evs), len(got))
	}
}
