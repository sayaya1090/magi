package app

import (
	"context"
	"sync"
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

// stuckLLM ignores cancellation: it holds the run until the test lets go, the way a tool or a
// backend that does not honour its context would hold a shutdown.
type stuckLLM struct {
	entered, release chan struct{}
	once             sync.Once
}

func (s *stuckLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	ch := make(chan port.ProviderEvent)
	close(ch)
	return ch, nil
}

// A shutdown waits for in-flight work only so long: past the grace it returns and names what it
// left behind, rather than hanging the process (and every deferred cleanup after it) forever.
func TestShutdownIsBoundedAndNamesWhatItLeft(t *testing.T) {
	llm := &stuckLLM{entered: make(chan struct{}), release: make(chan struct{})}
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	t.Cleanup(func() { close(llm.release) })
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the backend")
	}
	start := time.Now()
	left := a.Shutdown(100 * time.Millisecond)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("shutdown took %v past a 100ms grace", took)
	}
	if len(left) != 1 || left[0] != sid {
		t.Fatalf("shutdown should name the session it could not settle; got %v want [%s]", left, sid)
	}
}

// With nothing running a shutdown returns at once and leaves nothing behind.
func TestShutdownWithNothingRunningLeavesNothing(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &stuckLLM{entered: make(chan struct{}), release: make(chan struct{})}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	if left := a.Shutdown(time.Second); len(left) != 0 {
		t.Fatalf("nothing was running, yet shutdown left %v", left)
	}
}

// politeLLM holds the run until its context ends — a backend that DOES honour cancellation.
type politeLLM struct {
	entered chan struct{}
	once    sync.Once
}

func (p *politeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	p.once.Do(func() { close(p.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// A shutdown is not the person pressing stop. A turn cut by it must read as interrupted and
// resumable — not as abandoned, which is what a cancel by the person records and what recovery
// (rightly) never picks up again.
func TestATurnCutByShutdownIsResumableNotAbandoned(t *testing.T) {
	llm := &politeLLM{entered: make(chan struct{})}
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the backend")
	}
	if left := a.Shutdown(5 * time.Second); len(left) != 0 {
		t.Fatalf("a backend that honours cancel should settle within the grace; left %v", left)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if countType(evs, event.TypePromptAbandoned) != 0 {
		t.Fatalf("a shutdown recorded the prompt as abandoned (a person's cancel): %v", typesOf(evs))
	}
	var ed event.ErrorData
	if !lastData(evs, event.TypeError, &ed) || ed.Code != "interrupted" || !ed.Resumable {
		t.Fatalf("want an interrupted, resumable error; got %+v types=%v", ed, typesOf(evs))
	}
	fresh := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	if _, ok := fresh.ResumableTurnOf(ctx, sid); !ok {
		t.Fatal("the next start should read the shut-down turn as resumable")
	}
}
