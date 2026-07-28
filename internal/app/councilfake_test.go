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
	// judge scripts the revision-addressed verdict; nil means "always addressed" (the
	// default, so existing multi-round plan-audit fixtures still loop to the round cap).
	// judgeCalls counts how many times JudgeRevision ran (0 proves the flag gated it off).
	judge      func(port.RevisionJudgeRequest) port.RevisionVerdict
	judgeCalls int
	judgeReqs  []port.RevisionJudgeRequest
}

func (f *fakeCouncil) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
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

func (f *fakeCouncil) JudgeRevision(ctx context.Context, req port.RevisionJudgeRequest) (port.RevisionVerdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.judgeCalls++
	f.judgeReqs = append(f.judgeReqs, req)
	if f.judge != nil {
		return f.judge(req), nil
	}
	return port.RevisionVerdict{Addressed: true, Reason: "fake: addressed"}, nil
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
