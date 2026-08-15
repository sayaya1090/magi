package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// erroringLLM always fails the request (like a persistent 429), counting calls.
type erroringLLM struct{ calls atomic.Int64 }

func (f *erroringLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.calls.Add(1)
	return nil, errors.New("llm: status 429 after 3 attempts: rate limit")
}

// A persistent provider error must NOT trigger a re-run storm: the turn ends with
// one error event, and the loop does not keep re-invoking the failing backend.
func TestNoRetryStormOnPersistentError(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &erroringLLM{}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeError) < 1 {
		t.Fatalf("expected an error event, got %v", typesOf(got))
	}
	// Give any (buggy) re-run loop a moment to spin.
	time.Sleep(200 * time.Millisecond)
	if n := llm.calls.Load(); n > 2 {
		t.Errorf("LLM called %d times — re-run storm on persistent error (want ~1)", n)
	}
}

// A run killed by a persistent provider error still writes a terminal turn.finished. handoffAnswer
// keys on a PERSISTED turn.finished, so without this a handed-over piece whose worker hit a 429/5xx
// left the asker told "still thinking" forever, its receipt orphaned. The loop writes only a
// TypeError on that path; the run's retirement adds the finish. Reverting that fails this.
func TestAProviderErrorStillWritesATurnFinished(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, &erroringLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		evs, _ := store.Read(ctx, sid, 0)
		if countType(evs, event.TypeError) >= 1 && countType(evs, event.TypeTurnFinished) >= 1 {
			return // an error AND a terminal finish — a handoff asker resolves
		}
		time.Sleep(10 * time.Millisecond)
	}
	evs, _ := store.Read(ctx, sid, 0)
	t.Fatalf("no turn.finished after a provider error — a handoff asker would wait forever; events=%v", typesOf(evs))
}
