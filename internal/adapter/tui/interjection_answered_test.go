package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A message typed mid-turn is pinned below the answer it interrupted — that is what keeps the
// in-flight reply from appearing under a question nobody had asked yet. But the pin has to be
// released when the message stops being pending, and until this signal existed nothing could
// release it: the queue lives in app memory, the transcript is all the display layer reads, and
// the only clearer ran at turn end. So a request the model had ALREADY answered kept its "waiting"
// dot and sat glued to the bottom of the transcript for the rest of the turn, unable to scroll
// away with the history it belonged to.
func TestAnAnsweredInterjectionLeavesTheTailAndLosesTheWaitingGlyph(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.blocks = []block{
		{kind: blockUser, text: "countrows", reqID: "r1"},
		{kind: blockUser, text: "headercheck", reqID: "r2", queued: true},
		{kind: blockAssistant, text: "the header is on line one"},
	}
	d, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: "r2"})
	m.applyEvent(event.Event{Type: event.TypeInterjectionAnswered, Data: d})

	// It is no longer waiting…
	for _, b := range m.blocks {
		if b.reqID == "r2" && b.queued {
			t.Error("an answered interjection must lose the waiting glyph")
		}
	}
	// …and it sits just above the answer, not after it.
	var iq, ia int
	for i, b := range m.blocks {
		switch {
		case b.reqID == "r2":
			iq = i
		case b.kind == blockAssistant:
			ia = i
		}
	}
	if iq > ia {
		t.Errorf("the question belongs above the answer that covered it (q=%d a=%d)", iq, ia)
	}
	// And with the flag cleared it is an ordinary block, so the queued-tail hoist stops moving it.
	if _, moved := hoistQueuedToTail(m.blocks); moved {
		t.Error("nothing is queued any more — the tail hoist must not touch it")
	}
	out := m.transcript()
	if strings.Index(out, "headercheck") > strings.Index(out, "the header is on line one") {
		t.Errorf("rendered order still puts the question last:\n%s", out)
	}
}

// A bubble that is already in the right place must still lose the glyph — being positioned is not
// the same as being handled, and moveUserBlockBefore clears the flag only when it actually moves.
func TestTheGlyphClearsEvenWhenNothingNeedsMoving(t *testing.T) {
	m := newTestModel(t)
	m.blocks = []block{
		{kind: blockUser, text: "headercheck", reqID: "r2", queued: true},
		{kind: blockAssistant, text: "answered it"},
	}
	d, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: "r2"})
	m.applyEvent(event.Event{Type: event.TypeInterjectionAnswered, Data: d})
	if m.blocks[0].queued {
		t.Error("already above the answer, but still marked waiting")
	}
}

// An id that matches nothing is a no-op: a claim can arrive for a message this view never rendered
// (a resumed session, a switched pane), and it must not reorder somebody else's blocks.
func TestAClaimForAnUnknownMessageChangesNothing(t *testing.T) {
	m := newTestModel(t)
	m.blocks = []block{
		{kind: blockUser, text: "countrows", reqID: "r1"},
		{kind: blockAssistant, text: "305 rows"},
	}
	before := append([]block(nil), m.blocks...)
	d, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: "nope"})
	m.applyEvent(event.Event{Type: event.TypeInterjectionAnswered, Data: d})
	for i := range before {
		if m.blocks[i].text != before[i].text || m.blocks[i].queued != before[i].queued {
			t.Fatalf("an unknown id must not touch the transcript: %+v", m.blocks)
		}
	}
}
