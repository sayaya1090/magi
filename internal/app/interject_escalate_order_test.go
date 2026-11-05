package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A queued request that needs work runs as its own turn, and the mini-turn that decided so usually
// says a line first ("queued — doing it next"). That line used to be written the moment it was said,
// and the request was re-emitted only afterwards, so the transcript read reply-then-request: the
// answer to a question that had not been asked yet. Reported from a live session exactly that way.
//
// The request comes first; whatever the triage said about it follows.
func TestAnEscalatedRequestIsWrittenBeforeWhatTriageSaidAboutIt(t *testing.T) {
	const ack = "ACK queued, I will take it next"
	llm := steersLLM(func(aside string) bool { return strings.Contains(aside, "WWW") })
	llm.routeAck = ack
	a, sid := runWithSteersOn(t, llm, "WWW fix the ordering")

	promptSeq, ackSeq := int64(-1), int64(-1)
	for _, e := range readEvents(t, a, sid) {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom != "" && strings.Contains(partsText(d.Parts), "WWW") && promptSeq < 0 {
				promptSeq = e.Seq
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(d.Part.Text, "ACK") && ackSeq < 0 {
				ackSeq = e.Seq
			}
		}
	}
	if promptSeq < 0 {
		t.Fatal("the work item never re-surfaced as its own turn")
	}
	if ackSeq < 0 {
		t.Fatal("what the triage said was dropped instead of kept after the request")
	}
	if ackSeq < promptSeq {
		t.Fatalf("reply at seq %d was written before the request it is about (seq %d)", ackSeq, promptSeq)
	}
	t.Logf("PASS request seq %d precedes triage reply seq %d", promptSeq, ackSeq)
}
