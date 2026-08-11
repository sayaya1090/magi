package event

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// CompactionData.Reduction reports freed tokens and a rounded percent, clamps a
// non-shrinking compaction to 0, and never divides by a zero pre-compaction size.
func TestCompactionReduction(t *testing.T) {
	cases := []struct {
		before, after  int
		wantFreed, pct int
	}{
		{12000, 4000, 8000, 67}, // 8000/12000 = 66.7% → rounds to 67
		{1000, 750, 250, 25},
		{100, 100, 0, 0}, // no shrink
		{100, 120, 0, 0}, // grew → clamp freed at 0, no negative pct
		{0, 0, 0, 0},     // empty before → no divide-by-zero
		{3, 2, 1, 33},    // 1/3 = 33.3% → 33
		{3, 1, 2, 67},    // 2/3 = 66.7% → 67
	}
	for _, c := range cases {
		d := CompactionData{TokensBefore: c.before, TokensAfter: c.after}
		freed, pct := d.Reduction()
		if freed != c.wantFreed || pct != c.pct {
			t.Errorf("Reduction(%d→%d) = (%d,%d), want (%d,%d)",
				c.before, c.after, freed, pct, c.wantFreed, c.pct)
		}
	}
}

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
		{"transient-tool-progress", TypeToolProgress, true},
		{"transient-permission-requested", TypePermissionRequested, true},
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

// TurnFinishedData carries the execution-evidence gate's UNVERIFIED label. It must be
// omitted on a normal (verified) finish so existing consumers see the unchanged shape,
// and round-trip faithfully when the gate could not confirm the current version was run.
func TestTurnFinishedUnverifiedRoundTrip(t *testing.T) {
	// Verified (common) case: both fields omitted.
	b, err := json.Marshal(TurnFinishedData{Usage: Usage{In: 10, Out: 5}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "unverified") || strings.Contains(string(b), "reason") {
		t.Errorf("a verified finish must omit the unverified/reason keys, got %s", b)
	}

	// Unverified case: label + reason both present and preserved.
	b, err = json.Marshal(TurnFinishedData{Usage: Usage{In: 1}, Unverified: true, Reason: "never run"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TurnFinishedData
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Unverified || got.Reason != "never run" {
		t.Errorf("unverified round-trip: got %+v", got)
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
	for _, ty := range []Type{TypePermissionRequested, TypeCouncilDecided, TypeTurnFinished, TypeTodosChanged, TypePromptSubmitted} {
		if ty.Droppable() {
			t.Errorf("%s must NOT be droppable", ty)
		}
	}
}

// Invariant: every droppable type MUST be transient. A fact that is droppable would be
// silently lost under backpressure, desyncing the persisted log from the live UI. This
// loop catches a future fact accidentally added to droppableTypes.
func TestDroppableImpliesTransient(t *testing.T) {
	for ty := range droppableTypes {
		if !ty.IsTransient() {
			t.Errorf("%s is droppable but NOT transient — a fact must never be droppable", ty)
		}
	}
}
