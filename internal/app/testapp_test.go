package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// newOrchApp builds an App over a temp jsonl store with the given provider and config.
func newOrchApp(t *testing.T, llm port.LLMProvider, cfg Config) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, llm, builtin.Default(), bus.New(), nil, cfg)
}

// storeApp builds an App over a real jsonl store, torn down cleanly, and hands back the store so a
// test can read the log it wrote.
func storeApp(t *testing.T) (*App, *jsonl.Store) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	t.Cleanup(func() {
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	})
	return a, store
}

func parentSession(wd string) session.Session {
	return session.Session{ID: "s_parent", Workdir: wd, Agent: "default", Model: session.ModelRef{Model: "m"}}
}

// seedSession writes the mandatory session.created so subsequent appends are accepted.
func seedSession(t *testing.T, a *App, sid session.SessionID) {
	t.Helper()
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(context.Background(), sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorUser, ID: "cli"}, scd); err != nil {
		t.Fatal(err)
	}
}
