package event

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// F-EVENT-FACT-TRANSIENT: fact vs transient classification.
func TestTypeClassification(t *testing.T) {
	tests := []struct {
		name        string
		typ         Type
		wantTransit bool
	}{
		{"fact-session-created", TypeSessionCreated, false},
		{"fact-part-appended", TypePartAppended, false},
		{"fact-compaction", TypeCompaction, false},
		{"fact-turn-finished", TypeTurnFinished, false},
		{"fact-todos-changed", TypeTodosChanged, false},
		{"transient-part-delta", TypePartDelta, true},
		{"transient-tool-started", TypeToolStarted, true},
		{"transient-permission-requested", TypePermissionRequested, true},
		{"transient-agent-spawned", TypeAgentSpawned, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.typ.IsTransient(); got != tc.wantTransit {
				t.Errorf("IsTransient(%q)=%v, want %v", tc.typ, got, tc.wantTransit)
			}
			if got := tc.typ.IsFact(); got == tc.wantTransit {
				t.Errorf("IsFact(%q)=%v, want %v", tc.typ, got, !tc.wantTransit)
			}
		})
	}
}

// F-EVENT-FACT-TRANSIENT roundtrip-1: Event -> JSON -> Event is lossless.
func TestEventRoundTrip(t *testing.T) {
	data, err := json.Marshal(PartAppendedData{
		MessageID: "m2",
		Role:      session.RoleAssistant,
		Part: session.Part{
			ID:       "p2",
			Kind:     session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: json.RawMessage(`{"path":"x"}`)},
		},
	})
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	orig := Event{
		Seq:       3,
		SessionID: "s_01",
		Type:      TypePartAppended,
		Actor:     Actor{Kind: ActorAgent, ID: "default"},
		TS:        time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC),
		Data:      data,
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if got.Seq != orig.Seq || got.SessionID != orig.SessionID || got.Type != orig.Type ||
		got.Actor != orig.Actor || !got.TS.Equal(orig.TS) {
		t.Errorf("envelope mismatch:\n got=%+v\nwant=%+v", got, orig)
	}

	// Data payload should survive too.
	var gotData PartAppendedData
	if err := json.Unmarshal(got.Data, &gotData); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	var wantData PartAppendedData
	_ = json.Unmarshal(orig.Data, &wantData)
	if !reflect.DeepEqual(gotData, wantData) {
		t.Errorf("payload mismatch:\n got=%+v\nwant=%+v", gotData, wantData)
	}
}

// Droppable marks high-volume streaming events that may be dropped under backpressure;
// low-volume state transitions must not be.
func TestDroppable(t *testing.T) {
	for _, ty := range []Type{TypePartDelta, TypeToolProgress, TypeContextUsage, TypeCouncilDeliberating} {
		if !ty.Droppable() {
			t.Errorf("%s should be droppable", ty)
		}
	}
	for _, ty := range []Type{TypeAgentStatus, TypeCouncilDecided, TypeTurnFinished, TypeTodosChanged, TypePromptSubmitted} {
		if ty.Droppable() {
			t.Errorf("%s must NOT be droppable", ty)
		}
	}
}
