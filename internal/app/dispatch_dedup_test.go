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

// completingLLM emits a single terminal text turn (no tool calls) and closes, so
// any dispatched subagent finishes immediately with a trivial reply.
type completingLLM struct{}

func (completingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "Hi! How can I help?"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A re-dispatch of the SAME (agent, prompt) that has already COMPLETED is refused:
// a context-free delegate re-run with an identical prompt only reproduces the same
// result, so a weak orchestrator that keeps re-delegating the exact same degenerate
// task (e.g. handing "hi" to a coder over and over) would livelock. Its finished
// result is already in the conversation; re-dispatch is a no-op. A different prompt
// to the same agent is still accepted.
func TestDispatchDedupsCompleted(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"coder": {Name: "coder"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		closeCtx, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_ = a.Close(closeCtx)
	}()

	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "coder", Prompt: "hi"}); note != "" {
		t.Fatalf("first dispatch should be accepted, got note %q", note)
	}
	// Wait for the coder to finish and its result to be injected (outstanding→0).
	deadline := time.Now().Add(5 * time.Second)
	for a.bgOutstanding(parent.ID) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("coder did not complete in time")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Re-dispatching the identical (agent, prompt) after completion must be refused —
	// otherwise the orchestrator can re-run the same useless task forever.
	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "coder", Prompt: "hi"}); note == "" {
		t.Error("identical re-dispatch after completion should be refused (livelock guard)")
	}
	// A genuinely different task to the same agent is still allowed.
	if note := a.dispatch(ctx, parent, 0, port.SpawnRequest{Agent: "coder", Prompt: "review the explorer's findings"}); note != "" {
		t.Errorf("different prompt to same agent should be accepted, got note %q", note)
	}
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
