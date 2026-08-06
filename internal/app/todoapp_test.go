package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// newTodoApp is a bare App with one session and one user prompt in it — enough for the todo
// bookkeeping, which touches no model at all.
func newTodoApp(t *testing.T, cfg Config) (*App, session.SessionID) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, silentLLM{}, nil, bus.New(), nil, cfg))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "do A and B"}},
	})
	_ = a.appendFact(context.Background(), sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorUser, ID: "u"}, pd)
	return a, sid
}

// silentLLM answers nothing at all: a closed stream, the shape a caller must survive.
type silentLLM struct{}

func (silentLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	close(ch)
	return ch, nil
}

// countEvents reports how many events a session holds — for assertions that a path appended
// nothing.
func countEvents(t *testing.T, a *App, sid session.SessionID) int {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}
