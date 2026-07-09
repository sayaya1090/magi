package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
)

// A subagent's escalate() blocks until the orchestrator's reply is routed back.
func TestEscalateRoundTrip(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	parent, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	ansCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		ans, err := a.escalate(context.Background(), parent, "coder", "X or Y?")
		ansCh <- ans
		errCh <- err
	}()

	// Wait for the ask to register, then deliver the orchestrator's reply.
	for i := 0; i < 200; i++ {
		a.mu.Lock()
		pending := a.stateLocked(parent).pendingAsk != nil
		a.mu.Unlock()
		if pending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !a.answerPendingAsk(parent, "use X") {
		t.Fatal("expected a pending ask to answer")
	}
	select {
	case ans := <-ansCh:
		if ans != "use X" {
			t.Fatalf("escalate returned %q, want 'use X'", ans)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("escalate did not return after the answer")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("escalate error: %v", err)
	}
}

// A second concurrent ask to the same orchestrator is rejected (serialized).
func TestEscalateRejectsConcurrent(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	parent, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	go a.escalate(context.Background(), parent, "coder", "first?")
	for i := 0; i < 200; i++ {
		a.mu.Lock()
		pending := a.stateLocked(parent).pendingAsk != nil
		a.mu.Unlock()
		if pending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := a.escalate(context.Background(), parent, "tester", "second?"); err == nil {
		t.Fatal("expected the second concurrent ask to be rejected")
	}
}
