package tui

import (
	"strings"
	"testing"
)

// A message typed while an answer is still streaming is added to the transcript at once, and the
// live text renders after every block — so the answer to the FIRST question appeared below the
// second one, and stayed there when the finished text landed (it is appended after the queued
// bubble too). Two exchanges read as one tangle. The queued bubble belongs last: below the answer
// it interrupted, above nothing.
func TestTheAnswerBeingStreamedStaysAboveTheMessageThatInterruptedIt(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.blocks = []block{
		{kind: blockUser, text: "countrows", reqID: "r1"},
		{kind: blockUser, text: "headercheck", reqID: "r2", queued: true},
	}
	m.liveText = "readingnow"

	out := m.transcript()
	live := strings.Index(out, "readingnow")
	queued := strings.Index(out, "headercheck")
	if live < 0 || queued < 0 {
		t.Fatalf("both must render:\n%s", out)
	}
	if live > queued {
		t.Errorf("the streaming answer to the first question rendered BELOW the message typed during it:\n%s", out)
	}
}

// …and it must still hold once the streamed text is a finalized block: the turn keeps appending
// its own output after the queued bubble, so without the hoist the interrupting question ends up
// above work it never asked for.
func TestTheTurnsOwnOutputStaysAboveTheQueuedMessage(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.blocks = []block{
		{kind: blockUser, text: "countrows", reqID: "r1"},
		{kind: blockUser, text: "headercheck", reqID: "r2", queued: true},
		{kind: blockAssistant, text: "rows305"},
	}

	out := m.transcript()
	answer := strings.Index(out, "rows305")
	queued := strings.Index(out, "headercheck")
	if answer < 0 || queued < 0 {
		t.Fatalf("both must render:\n%s", out)
	}
	if answer > queued {
		t.Errorf("the first question's answer rendered below the message that interrupted it:\n%s", out)
	}
	if got := m.blocks[len(m.blocks)-1].text; !strings.Contains(got, "headercheck") {
		t.Errorf("the queued bubble must be hoisted to the tail so its recorded line still ascends with its index, got last=%q", got)
	}
}

// The click mapping scans blockLineStart backwards for the first start at or before a line, which
// only answers correctly while the array ascends. Rendering the queued bubbles after the live
// section keeps that true — they are the last indices AND the last lines.
func TestBlockLineStartsStillAscend(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.blocks = []block{
		{kind: blockUser, text: "countrows", reqID: "r1"},
		{kind: blockAssistant, text: "working on it"},
		{kind: blockUser, text: "more1", reqID: "r2", queued: true},
		{kind: blockUser, text: "more2", reqID: "r3", queued: true},
	}
	m.liveText = "still reading"
	_ = m.transcript()

	if len(m.blockLineStart) != len(m.blocks) {
		t.Fatalf("every block needs a recorded start: got %d for %d blocks", len(m.blockLineStart), len(m.blocks))
	}
	for i := 1; i < len(m.blockLineStart); i++ {
		if m.blockLineStart[i] < m.blockLineStart[i-1] {
			t.Fatalf("block %d starts at line %d, before block %d at %d — clicks map to the wrong block",
				i, m.blockLineStart[i], i-1, m.blockLineStart[i-1])
		}
	}
}

// Nothing is queued in the ordinary case, so the ordering is exactly what it was.
func TestOrderIsUntouchedWithNothingQueued(t *testing.T) {
	blocks := []block{
		{kind: blockUser, text: "q", reqID: "r1"},
		{kind: blockAssistant, text: "a"},
	}
	got, moved := hoistQueuedToTail(blocks)
	if moved {
		t.Error("nothing is queued — the list must not be rebuilt")
	}
	if got[0].text != "q" || got[1].text != "a" {
		t.Errorf("order changed: %v", got)
	}
}
