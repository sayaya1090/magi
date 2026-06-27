package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeCouncil returns a scripted deliberation per round (the last repeats).
type fakeCouncil struct {
	delibs []council.Deliberation
	calls  int
}

func (f *fakeCouncil) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	i := f.calls
	f.calls++
	if i < len(f.delibs) {
		return f.delibs[i], nil
	}
	return f.delibs[len(f.delibs)-1], nil
}

// submitAndDrain creates a session, submits a prompt, and returns the events.
func submitAndDrain(t *testing.T, a *App, workdir string) []event.Event {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: workdir, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	return waitForTerminal(t, a, sid)
}

// The council gate holds the loop open until the council votes done: a "continue"
// round injects feedback and the loop runs again.
func TestCouncilGateContinuesThenFinishes(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "the tests are missing — add them"},
		{Round: 2, Decision: council.Done},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc}) // fakeLLM with no steps → always "done" text
	evs := submitAndDrain(t, a, wd)

	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2", got)
	}
	if got := countType(evs, event.TypeCouncilDecided); got != 2 {
		t.Fatalf("council.decided = %d, want 2", got)
	}
	// The continue round must inject the feedback as a council-authored prompt.
	injected := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "council" {
			var d event.PromptSubmittedData
			_ = json.Unmarshal(e.Data, &d)
			if len(d.Parts) > 0 && strings.Contains(d.Parts[0].Text, "add them") {
				injected = true
			}
		}
	}
	if !injected {
		t.Fatal("continue round should inject the council feedback as a prompt")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after the council votes done")
	}
}

// With no council configured, the loop finishes as before — no council events.
func TestNoCouncilNoGate(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	evs := submitAndDrain(t, a, wd)
	if countType(evs, event.TypeCouncilConvened) != 0 {
		t.Fatal("no council should be convened when unconfigured")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish normally")
	}
}

// A council that always says continue is bounded by max_rounds, then finishes.
func TestCouncilMaxRoundsStops(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		// Distinct feedback each round so the no-progress guard doesn't fire first;
		// the round cap is what must stop it.
		{Round: 1, Decision: council.Continue, Feedback: "step one"},
		{Round: 2, Decision: council.Continue, Feedback: "step two"},
		{Round: 3, Decision: council.Continue, Feedback: "step three"},
		{Round: 4, Decision: council.Continue, Feedback: "step four"},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, CouncilMaxRounds: 2})
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2 (max rounds)", got)
	}
	if !hasDecidedNote(evs, "unresolved after") {
		t.Fatal("hitting the cap should record a forced-finish council.decided note")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after rounds are exhausted")
	}
}

// Repeated feedback (no progress) stops the gate before max_rounds.
func TestCouncilNoProgressStops(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "same thing"},
		{Round: 2, Decision: council.Continue, Feedback: "same thing"},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, CouncilMaxRounds: 5})
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2 (stopped on no progress)", got)
	}
	if !hasDecidedNote(evs, "no new feedback") {
		t.Fatal("repeated feedback should record a no-progress council.decided note")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after no-progress is detected")
	}
}

func hasDecidedNote(evs []event.Event, sub string) bool {
	for _, e := range evs {
		if e.Type == event.TypeCouncilDecided {
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(d.Note, sub) {
				return true
			}
		}
	}
	return false
}
