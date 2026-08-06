package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func unfPrompt(id, text string) event.Event {
	d, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	return event.Event{Type: event.TypePromptSubmitted, Data: d}
}

func unfToolCall(name string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: name}}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func unfFinished() event.Event { return event.Event{Type: event.TypeTurnFinished, Data: []byte(`{}`)} }
func unfError() event.Event    { return event.Event{Type: event.TypeError, Data: []byte(`{}`)} }

// A turn cut off mid-flight is visible in the log, and until now nothing looked.
//
// The record already held the answer — a prompt, tools that ran, and no turn.finished — but nobody
// asked the question, so a session resumed after a crash came back with the conversation intact and
// forty steps of work silently abandoned.
func TestAnAbandonedTurnIsFoundInTheLog(t *testing.T) {
	got, ok := unfinishedTurn([]event.Event{
		unfPrompt("m1", "first thing"), unfToolCall("read"), unfFinished(),
		unfPrompt("m2", "refactor the parser"), unfToolCall("read"), unfToolCall("edit"), unfToolCall("bash"),
	})
	if !ok {
		t.Fatal("a prompt with tools and no turn.finished was not reported as unfinished")
	}
	if got.MessageID != "m2" || got.Text != "refactor the parser" {
		t.Errorf("found %+v, want the LAST prompt", got)
	}
	// How much was in flight, so a caller can say what is at stake rather than just "something".
	if got.Steps != 3 {
		t.Errorf("Steps = %d, want the 3 tool calls the abandoned turn had made", got.Steps)
	}
}

// A finished turn is not unfinished, and neither is an empty log. Getting this wrong would offer to
// re-run work that is already done — the expensive direction of the two.
func TestAFinishedTurnIsNotOffered(t *testing.T) {
	for name, evs := range map[string][]event.Event{
		"empty log":     {},
		"finished":      {unfPrompt("m1", "x"), unfToolCall("read"), unfFinished()},
		"never started": {unfFinished()},
	} {
		if _, ok := unfinishedTurn(evs); ok {
			t.Errorf("%s was reported as an unfinished turn", name)
		}
	}
}

// A turn that ENDED IN AN ERROR is not rescued. The loop recorded why it stopped and the user saw
// it; re-running that is repeating a failure, not recovering work.
func TestATurnThatEndedInAnErrorIsNotResumed(t *testing.T) {
	if _, ok := unfinishedTurn([]event.Event{
		unfPrompt("m1", "do it"), unfToolCall("bash"), unfError(),
	}); ok {
		t.Error("a turn that ended in a recorded error was offered for resume")
	}
}

// Only the LAST prompt counts. A later prompt means the user moved on, and re-running something
// they superseded is worse than dropping it.
func TestASupersededTurnIsNotResumed(t *testing.T) {
	got, ok := unfinishedTurn([]event.Event{
		unfPrompt("m1", "the abandoned one"), unfToolCall("read"), // never finished
		unfPrompt("m2", "actually do this instead"), unfToolCall("read"),
	})
	if !ok {
		t.Fatal("the latest prompt is unfinished and was not reported")
	}
	if got.MessageID != "m1" && got.Text == "the abandoned one" {
		t.Error("the superseded prompt was chosen over the one the user actually last asked")
	}
	if got.MessageID != "m2" {
		t.Errorf("found %q, want the last prompt m2", got.MessageID)
	}
	// Steps count from the LAST prompt only — the abandoned turn's work is not this turn's.
	if got.Steps != 1 {
		t.Errorf("Steps = %d, want 1 (only the current turn's)", got.Steps)
	}
}
