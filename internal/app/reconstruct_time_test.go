package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A rebuilt message remembers when it happened.
//
// Every event in the log carries a timestamp and the rebuild threw it away, so the time of a
// message existed only in a UI that had been watching it arrive. The terminal stamped its blocks
// live and lost them on resume; the console never had them at all, because it only ever reads
// rebuilt messages. The log had recorded it the whole time.
func TestARebuiltMessageRemembersWhenItHappened(t *testing.T) {
	asked := time.Date(2026, 8, 11, 4, 5, 0, 0, time.UTC)
	answered := asked.Add(2 * time.Minute)
	part := func(text string) json.RawMessage {
		d, err := json.Marshal(event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: text}}})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	appended, err := json.Marshal(event.PartAppendedData{
		MessageID: "m2", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: "did it"}})
	if err != nil {
		t.Fatal(err)
	}

	msgs := reconstruct([]event.Event{
		{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser}, TS: asked, Data: part("go and do it")},
		{Type: event.TypePartAppended, TS: answered, Data: appended},
	})
	if len(msgs) != 2 {
		t.Fatalf("rebuilt %d messages", len(msgs))
	}
	if !msgs[0].At.Equal(asked) {
		t.Errorf("the prompt is stamped %v, want %v", msgs[0].At, asked)
	}
	if !msgs[1].At.Equal(answered) {
		t.Errorf("the answer is stamped %v, want %v", msgs[1].At, answered)
	}
}

// A message that grew over minutes is stamped with when it STARTED.
//
// A streamed answer arrives as many events; stamping each one would move the message's time
// forward as it was written, so a transcript read while it streamed would show a row whose time
// kept changing. What a reader means by "when did it say that" is when it began.
func TestAStreamedMessageIsStampedWithWhenItBegan(t *testing.T) {
	began := time.Date(2026, 8, 11, 4, 5, 0, 0, time.UTC)
	ev := func(at time.Time, text string) event.Event {
		d, err := json.Marshal(event.PartAppendedData{
			MessageID: "m1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: text}})
		if err != nil {
			t.Fatal(err)
		}
		return event.Event{Type: event.TypePartAppended, TS: at, Data: d}
	}
	msgs := reconstruct([]event.Event{ev(began, "first"), ev(began.Add(4*time.Minute), "second")})
	if len(msgs) != 1 {
		t.Fatalf("rebuilt %d messages, want the two parts folded into one", len(msgs))
	}
	if !msgs[0].At.Equal(began) {
		t.Errorf("the message is stamped %v, want the moment it began (%v)", msgs[0].At, began)
	}
}
