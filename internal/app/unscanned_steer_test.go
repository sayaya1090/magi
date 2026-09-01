package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A step answers the world as of its own interjection scan, and a message typed after it does not
// ride into that request.
//
// The mask over a mid-turn steer is built by ENQUEUEING it: the top-of-loop scan puts it in the
// queue, and liveEvents drops exactly what the queue holds. So the mask only ever covers prompts
// that scan saw. A step then re-reads the log twice more — for the arrival notes it appends, and
// after a compaction — and a steer landing in between comes back on those reads unscanned, unqueued
// and therefore unmasked. The request then carries the task the turn is on AND the message typed
// into the middle of it, side by side, with nothing marking them apart: a model asked two things
// and told nothing about the difference.
//
// Nothing is lost by holding it back. The next step's scan queues and masks it, and the finish
// boundary catches even the last one.
func TestASteerThatLandsMidStepDoesNotRideIntoThatRequest(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	// What the step scanned: the task it is running.
	if err := a.appendPrompt(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}); err != nil {
		t.Fatal(err)
	}
	scanned := highestSeq(readLog(t, a, sid))

	// Then, mid-step: an arrival note the step itself appends, and a steer the person types.
	pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_arr",
		Parts: []session.Part{{Kind: session.PartText, Text: "a skill became available"}}})
	if err := a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "arrivals"}, pd); err != nil {
		t.Fatal(err)
	}
	if err := a.appendPrompt(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "also check the header"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}); err != nil {
		t.Fatal(err)
	}

	got := a.rereadWithoutUnscannedSteers(ctx, sid, scanned)

	texts := promptTexts(t, got)
	if !texts["count the rows"] {
		t.Error("the task the step is running must survive the re-read")
	}
	if !texts["a skill became available"] {
		t.Error("the step's own arrival note is why the re-read happens; it must land")
	}
	if texts["also check the header"] {
		t.Error("a steer this step never scanned rode into its request — the model is now being " +
			"asked two things with nothing to tell them apart")
	}
	// And it is held back, not dropped: the log still has it for the next scan.
	if !promptTexts(t, readLog(t, a, sid))["also check the header"] {
		t.Fatal("the steer must still be in the log — the next step's scan owns it")
	}
}

// The hold-back is narrow: only what the person typed. Everything else a re-read brings is the
// step's own business — a compaction's rewritten history, tool results, system notes — and holding
// any of it would break the thing the re-read exists for.
func TestOnlyThePersonsWordsAreHeldBack(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	scanned := highestSeq(readLog(t, a, sid))

	for _, actor := range []event.Actor{
		{Kind: event.ActorSystem, ID: "arrivals"},
		{Kind: event.ActorSystem, ID: "council"},
		{Kind: event.ActorAgent, ID: "main"},
	} {
		pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_" + actor.ID,
			Parts: []session.Part{{Kind: session.PartText, Text: "from " + actor.ID}}})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted, actor, pd); err != nil {
			t.Fatal(err)
		}
	}
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: "working on it"}})
	if err := a.appendFact(ctx, sid, event.TypePartAppended,
		event.Actor{Kind: event.ActorAgent, ID: "main"}, d); err != nil {
		t.Fatal(err)
	}

	got := a.rereadWithoutUnscannedSteers(ctx, sid, scanned)
	texts := promptTexts(t, got)
	for _, want := range []string{"from arrivals", "from council", "from main"} {
		if !texts[want] {
			t.Errorf("%q is not the person typing; it must land", want)
		}
	}
	var sawAssistant bool
	for _, e := range got {
		if e.Type == event.TypePartAppended {
			sawAssistant = true
		}
	}
	if !sawAssistant {
		t.Error("the assistant's own text is not a steer and must survive")
	}
}

func readLog(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func promptTexts(t *testing.T, evs []event.Event) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			out[p.Text] = true
		}
	}
	return out
}
