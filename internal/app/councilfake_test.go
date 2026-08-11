package app

import (
	"context"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeCouncil returns a scripted deliberation per round (the last repeats) and
// records the last request it received.
type fakeCouncil struct {
	mu      sync.Mutex
	delibs  []council.Deliberation
	calls   int
	lastReq port.DeliberationRequest
	reqs    []port.DeliberationRequest // every request in order, so a re-round's carried context is assertable
	// onDeliberate runs INSIDE the call, for the tests whose subject is what is true while the
	// council sits — a live note, a state that must be cleared afterwards. Asserting after the
	// call returns cannot see any of it.
	onDeliberate func(req port.DeliberationRequest)
	app          *App
	sid          session.SessionID
}

func (f *fakeCouncil) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	if f.onDeliberate != nil {
		f.onDeliberate(req)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq = req
	f.reqs = append(f.reqs, req)
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

// mustRead returns the full fact log for a session.
func mustRead(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}
