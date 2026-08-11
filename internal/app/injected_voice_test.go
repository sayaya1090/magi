package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// prompt builds a submitted-prompt event from a given author.
func promptFrom(t *testing.T, actor event.Actor, id, text string) event.Event {
	t.Helper()
	d, err := json.Marshal(event.PromptSubmittedData{
		MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypePromptSubmitted, Actor: actor, Data: d}
}

// What magi says to the agent is not attributed to the person.
//
// Every submitted prompt used to reconstruct as a user message whatever had written it, and the
// actor — which the event has carried the whole time — was read only for display. So "You stopped
// without saying you are finished", a sentence magi writes about the agent's own behaviour,
// arrived indistinguishable from somebody typing it. The handoff delivery in the same package has
// always named itself; the orchestrator's nudges did not.
func TestWhatMagiSaysIsNotAttributedToThePerson(t *testing.T) {
	msgs := reconstruct([]event.Event{
		promptFrom(t, event.Actor{Kind: event.ActorUser, ID: "tui"}, "m1", "rename a.txt to b.txt"),
		promptFrom(t, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, "m2",
			"You stopped without saying you are finished."),
		promptFrom(t, event.Actor{Kind: event.ActorAgent, ID: "sub"}, "m3", "[subagent scout] found it"),
	})
	if len(msgs) != 3 {
		t.Fatalf("reconstructed %d messages, want 3", len(msgs))
	}
	if msgs[0].Role != session.RoleUser {
		t.Errorf("what the person typed is a %q message", msgs[0].Role)
	}
	if msgs[1].Role != session.RoleSystem {
		t.Errorf("what magi wrote is a %q message — the same role as the person's", msgs[1].Role)
	}
	// A subagent's report stays a user message: it IS work that arrived for the agent to use, and
	// it names itself in its own text.
	if msgs[2].Role != session.RoleUser {
		t.Errorf("a subagent's report is a %q message", msgs[2].Role)
	}
}

// The last thing the PERSON said is the person's, not the last thing magi said to them.
//
// lastUserText picks the retrieval query for the experience store. With every injected prompt
// wearing the user's role, a turn that had been nudged searched the store for magi's own nudge.
func TestTheLastThingThePersonSaidIsTheirs(t *testing.T) {
	msgs := reconstruct([]event.Event{
		promptFrom(t, event.Actor{Kind: event.ActorUser, ID: "tui"}, "m1", "make the button accessible"),
		promptFrom(t, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, "m2",
			"You stopped without saying you are finished."),
	})
	if got := lastUserText(msgs); got != "make the button accessible" {
		t.Errorf("the last thing the person said reads as %q", got)
	}
}
