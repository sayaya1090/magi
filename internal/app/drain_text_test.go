package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// stallLLM streams NOTHING until its ctx is cancelled, then closes — modelling a wedged backend that
// accepts the request and goes silent (what real providers do once the request ctx is cancelled).
type stallLLM struct{}

func (stallLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		<-ctx.Done() // silent until the guard cancels the request
		close(ch)
	}()
	return ch, nil
}

// spinLLM streams reasoning tokens FOREVER (never text, never closing) until its ctx is cancelled —
// modelling the model spinning in thought, which keeps resetting the idle watchdog so only the
// reasoning-byte cap can stop it.
type spinLLM struct{}

func (spinLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		chunk := port.ProviderEvent{Type: port.ProviderReasoning, Text: "thinking thinking thinking "}
		for {
			select {
			case <-ctx.Done():
				return
			case ch <- chunk:
			}
		}
	}()
	return ch, nil
}

// GuardProvider must abort a SILENT stream at its idle bound (2x streamStallTimeout) — not hang until
// the turn wall clock — so a hung planner/council/side-call generate unwinds in seconds. This is the
// universal guard that replaces the per-consumer watchdogs.
func TestGuardProviderAbortsSilentStream(t *testing.T) {
	old := streamStallTimeout
	streamStallTimeout = 20 * time.Millisecond // providerGuardIdle = 40ms
	defer func() { streamStallTimeout = old }()

	stream, err := GuardProvider(stallLLM{}).StreamChat(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("guard StreamChat error: %v", err)
	}
	start := time.Now()
	for range stream { // drain until the guard aborts and closes the stream
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the guard must close a silent stream near its idle bound, took %s (hung?)", elapsed)
	}
}

// GuardProvider must abort a reasoning SPIN via the byte cap (the idle watchdog can't — every reasoning
// token resets it), so a thought spin unwinds instead of running to the wall clock.
func TestGuardProviderAbortsReasoningSpin(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "2000") // providerGuardCap = 4000
	old := streamStallTimeout
	streamStallTimeout = 5 * time.Second // idle far away, so only the byte cap can stop this
	defer func() { streamStallTimeout = old }()

	stream, err := GuardProvider(spinLLM{}).StreamChat(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("guard StreamChat error: %v", err)
	}
	start := time.Now()
	for range stream {
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the spin cap must abort a reasoning flood quickly, took %s", elapsed)
	}
}

// GuardProvider is a transparent pass-through for a normal reply — it forwards every event unchanged.
func TestGuardProviderForwardsNormalReply(t *testing.T) {
	got := ""
	stream, err := GuardProvider(&gateLLM{text: "the plan JSON"}).StreamChat(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatalf("guard StreamChat error: %v", err)
	}
	for ev := range stream {
		if ev.Type == port.ProviderText {
			got += ev.Text
		}
	}
	if got != "the plan JSON" {
		t.Errorf("guard must forward the reply unchanged, got %q", got)
	}
}

// drainText (now guard-free) accumulates the reply's text via the guarded provider.
func TestDrainTextAccumulates(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "the plan JSON"}, Config{Permission: "allow", MaxAgents: 10})
	text, err := a.drainText(context.Background(), AgentSpec{}, port.ChatRequest{})
	if err != nil || text != "the plan JSON" {
		t.Fatalf("drainText = %q, %v; want \"the plan JSON\", nil", text, err)
	}
}
