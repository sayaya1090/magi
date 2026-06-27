package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// blockingLLM keeps every call running until the context is cancelled, so a
// dispatched subagent stays in-flight for the duration of the test.
type blockingLLM struct{}

func (blockingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// A re-dispatch of the SAME (agent, prompt) while one is already running is
// refused (returns a note, doesn't spawn a duplicate); a different prompt to the
// same agent is allowed.
func TestDispatchDedupsInflight(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, blockingLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel() // unblock the in-flight subagents first…
		closeCtx, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_ = a.Close(closeCtx) // …then wait for their goroutines to drain before TempDir cleanup
	}()

	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "X"}); note != "" {
		t.Fatalf("first dispatch should be accepted, got note %q", note)
	}
	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "X"}); note == "" {
		t.Error("identical re-dispatch should be refused while in-flight")
	}
	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "Y"}); note != "" {
		t.Errorf("different prompt to same agent should be accepted, got note %q", note)
	}
	if n := a.bgOutstanding(parent.ID); n != 2 {
		t.Errorf("expected 2 outstanding (X and Y), got %d", n)
	}
}
