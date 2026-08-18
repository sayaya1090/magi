package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The headless stream is a JSON fact per LINE, and nothing bounded a line.
//
// Measured 2026-08-18: a run at the model's own default reasoning depth emitted one
// `part.appended` of 142,410 bytes. The reader on the other end — asyncio's readline, which is what
// anything driving a container through a pipe tends to use — raised "Separator is not found, and
// chunk exceed the limit" and the whole trial died with it. The JSON was valid. It was one line too
// long to be read as a line.
func TestALongReasoningPartStaysOneReadableLine(t *testing.T) {
	huge := strings.Repeat("thinking about the metacircular evaluator. ", 4000) // ~168 KB
	d, err := json.Marshal(event.PartAppendedData{
		MessageID: "m_1", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartReasoning, Text: huge},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := event.Event{Type: event.TypePartAppended, Data: d}

	b, err := json.Marshal(boundedForLine(e))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxJSONLine {
		t.Errorf("the line is %d bytes; the bound is %d", len(b), maxJSONLine)
	}
	// Still one line — a clip that introduced a newline would split one fact into two.
	if strings.Contains(string(b), "\n") {
		t.Error("the clipped event carries a raw newline")
	}
	// Still the same fact, readable by the same consumer.
	var back event.Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("the clipped event is no longer valid JSON: %v", err)
	}
	var got event.PartAppendedData
	if err := json.Unmarshal(back.Data, &got); err != nil {
		t.Fatalf("the payload no longer parses: %v", err)
	}
	// The cut is in the text, not in a flag beside it: a consumer that shows the field would
	// otherwise present a truncation as the model's own words trailing off.
	if !strings.Contains(got.Part.Text, "cut so this stays one readable line") {
		t.Error("the cut is silent, so the reasoning reads as if it simply stopped")
	}
	if !strings.Contains(got.Part.Text, "the log has all of it") {
		t.Error("the note does not say where the rest is")
	}
}

// Everything that already fits crosses unchanged. A bound that rewrote ordinary events would put a
// second version of the log on the wire.
func TestAnEventThatFitsIsUntouched(t *testing.T) {
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "m_1", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartReasoning, Text: "short thought"},
	})
	e := event.Event{Type: event.TypePartAppended, Data: d}
	if got := boundedForLine(e); string(got.Data) != string(e.Data) {
		t.Error("an event well under the bound was rewritten")
	}
}

// Only reasoning is shortened. A tool result is capped where it is produced and a text part is the
// answer itself; mangling either to fit a line would be losing the thing the run is for.
func TestOnlyReasoningIsShortened(t *testing.T) {
	huge := strings.Repeat("x", maxJSONLine*2)
	for _, kind := range []session.PartKind{session.PartText, session.PartToolResult} {
		d, _ := json.Marshal(event.PartAppendedData{
			MessageID: "m_1", Role: session.RoleAssistant,
			Part: session.Part{Kind: kind, Text: huge},
		})
		e := event.Event{Type: event.TypePartAppended, Data: d}
		if got := boundedForLine(e); len(got.Data) != len(e.Data) {
			t.Errorf("a %s part was shortened; only reasoning may be", kind)
		}
	}
}
