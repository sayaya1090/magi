package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A supervisor's time only scales if each intervention removes future ones, and the material for
// that is the set of moments a person stepped in. Nothing collected them: a steer is an ordinary
// user prompt and dissolves into the transcript, indistinguishable from a new instruction.
//
// Derived rather than recorded, so it answers for logs written before the question was asked — a
// field added today describes nothing that happened yesterday, and yesterday is what a supervisor
// wants to look at.

func evAt(t *testing.T, typ event.Type, actor event.ActorKind, at time.Time, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b, TS: at, Actor: event.Actor{Kind: actor, ID: "x"}}
}

func humanPrompt(t *testing.T, at time.Time, text string) event.Event {
	return evAt(t, event.TypePromptSubmitted, event.ActorUser, at, event.PromptSubmittedData{
		MessageID: "m", Parts: []session.Part{{Kind: session.PartText, Text: text}}})
}

// The distinction is in the log already: a prompt that arrives while a turn is OPEN is a steer,
// because the person did not wait for the answer. One that arrives after turn.finished is the next
// task.
func TestASteerIsAPromptThatDidNotWait(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	evs := []event.Event{
		humanPrompt(t, t0, "port the rate limiter"),
		evAt(t, event.TypePartAppended, event.ActorAgent, t0.Add(10*time.Second), event.PartAppendedData{MessageID: "a"}),
		humanPrompt(t, t0.Add(30*time.Second), "no, not that file"), // mid-turn: a steer
		evAt(t, event.TypeTurnFinished, event.ActorAgent, t0.Add(time.Minute), event.TurnFinishedData{}),
		humanPrompt(t, t0.Add(2*time.Minute), "now write the docs"), // after: a new task
	}
	got := interventions(evs, "s1")
	if len(got) != 1 {
		t.Fatalf("found %d interventions, want the one that did not wait: %+v", len(got), got)
	}
	if got[0].Kind != "steer" || got[0].Text != "no, not that file" {
		t.Errorf("the intervention came back as %+v", got[0])
	}
	// How long the turn had been running when it arrived: a steer three seconds in corrects the
	// instruction, one twenty minutes in corrects the work, and they promote to different things.
	if got[0].AfterSec != 30 {
		t.Errorf("the steer landed %ds into the turn, want 30", got[0].AfterSec)
	}
}

// A refusal is the shortest correction there is.
func TestADenialCounts(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	evs := []event.Event{
		humanPrompt(t, t0, "clean the tree"),
		evAt(t, event.TypePermissionDecided, event.ActorUser, t0.Add(5*time.Second),
			event.PermissionDecidedData{CallID: "bash", Decision: "allow"}),
		evAt(t, event.TypePermissionDecided, event.ActorUser, t0.Add(9*time.Second),
			event.PermissionDecidedData{CallID: "rm-rf", Decision: "deny"}),
	}
	got := interventions(evs, "s1")
	if len(got) != 1 || got[0].Kind != "denied" || got[0].Text != "rm-rf" {
		t.Fatalf("interventions came back as %+v — an allow is the ordinary course, a deny is not", got)
	}
}

// magi's own prompts are not the supervisor's.
//
// It injects several — the finish-declaration nudge, a resurfaced interjection, the note that
// nobody answered a permission — and counting those would have a supervisor promoting magi's advice
// to itself into a rule about how people work.
func TestMagisOwnPromptsAreNotInterventions(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	evs := []event.Event{
		humanPrompt(t, t0, "do the thing"),
		evAt(t, event.TypePromptSubmitted, event.ActorSystem, t0.Add(20*time.Second),
			event.PromptSubmittedData{MessageID: "n", Parts: []session.Part{{Kind: session.PartText,
				Text: "You stopped without saying you are finished."}}}),
		evAt(t, event.TypePromptSubmitted, event.ActorAgent, t0.Add(25*time.Second),
			event.PromptSubmittedData{MessageID: "n2", Parts: []session.Part{{Kind: session.PartText,
				Text: "a subagent's result"}}}),
	}
	if got := interventions(evs, "s1"); len(got) != 0 {
		t.Errorf("magi's own prompts were counted as a person stepping in: %+v", got)
	}
}

// A turn that ended in an error, or was abandoned, is over — the next prompt is a new task and not
// a correction to something still running.
func TestATurnThatEndedBadlyStillEnds(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	for _, ending := range []event.Type{event.TypeError, event.TypePromptAbandoned} {
		evs := []event.Event{
			humanPrompt(t, t0, "first"),
			evAt(t, ending, event.ActorAgent, t0.Add(10*time.Second), map[string]string{"message": "boom"}),
			humanPrompt(t, t0.Add(20*time.Second), "try it another way"),
		}
		if got := interventions(evs, "s1"); len(got) != 0 {
			t.Errorf("after %s the next prompt was counted as a steer: %+v", ending, got)
		}
	}
}

// Several steers in one turn are several interventions: a person who had to say three things is
// three data points, and collapsing them would hide exactly the repetition worth promoting.
func TestEverySteerInATurnCounts(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	evs := []event.Event{
		humanPrompt(t, t0, "start"),
		humanPrompt(t, t0.Add(10*time.Second), "not like that"),
		humanPrompt(t, t0.Add(20*time.Second), "and run the tests"),
		humanPrompt(t, t0.Add(30*time.Second), "the OTHER tests"),
		evAt(t, event.TypeTurnFinished, event.ActorAgent, t0.Add(time.Minute), event.TurnFinishedData{}),
	}
	got := interventions(evs, "s1")
	if len(got) != 3 {
		t.Fatalf("three steers came back as %d: %+v", len(got), got)
	}
	if got[2].AfterSec != 30 {
		t.Errorf("the last steer is timed from the TURN's start, not the previous steer: %+v", got[2])
	}
}

// A question answered on the spot is not a correction.
//
// Anything a person says mid-turn counted as a steer, so "what model is this on?" — asked while
// something ran, answered in the reply, and changing nothing about the work — landed on the list a
// supervisor reads to decide what should become a rule. That list is only worth reading if
// everything on it is a moment the agent had to be corrected.
//
// The log already tells them apart, in two places, because there are two ways it happens: the
// agent routes the interjection as "answered", or its reply names the message it is answering.
func TestSomethingAnsweredOnTheSpotIsNotASteer(t *testing.T) {
	t0 := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	ask := func(at time.Time, id, text string) event.Event {
		return evAt(t, event.TypePromptSubmitted, event.ActorUser, at, event.PromptSubmittedData{
			MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	}
	evs := []event.Event{
		ask(t0, "m1", "port the rate limiter"),
		evAt(t, event.TypePartAppended, event.ActorAgent, t0.Add(5*time.Second), event.PartAppendedData{MessageID: "a1"}),
		// Asked mid-turn and simply answered: the agent said so through route_interjection.
		ask(t0.Add(10*time.Second), "m2", "what model is this on?"),
		evAt(t, event.TypeInterjectionAnswered, event.ActorSystem, t0.Add(12*time.Second),
			event.InterjectionAnsweredData{MessageID: "m2"}),
		// Asked mid-turn and answered inline: the reply names it instead.
		ask(t0.Add(20*time.Second), "m3", "how many steps has this taken?"),
		evAt(t, event.TypePartAppended, event.ActorAgent, t0.Add(22*time.Second),
			event.PartAppendedData{MessageID: "a2", InReplyTo: "m3"}),
		// And an actual correction, which is what the list is for.
		ask(t0.Add(30*time.Second), "m4", "no, not that file"),
		evAt(t, event.TypeTurnFinished, event.ActorAgent, t0.Add(time.Minute), event.TurnFinishedData{}),
	}
	got := interventions(evs, "s1")
	if len(got) != 1 {
		t.Fatalf("found %d interventions, want only the correction: %+v", len(got), got)
	}
	if got[0].Text != "no, not that file" {
		t.Errorf("the one that counted was %q", got[0].Text)
	}
}
