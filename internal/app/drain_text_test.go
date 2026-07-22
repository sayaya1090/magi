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
		<-ctx.Done() // silent until the drain watchdog cancels the request
		close(ch)
	}()
	return ch, nil
}

// drainText's inactivity watchdog must abort a silent stream at streamStallTimeout — not hang until the
// turn's wall clock — so a hung planner re-plan generate unwinds in seconds. This is the gap
// consumeStream's watchdog left open (the planner consumes the stream directly).
func TestDrainTextAbortsSilentStream(t *testing.T) {
	old := streamStallTimeout
	streamStallTimeout = 40 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	a := newOrchApp(t, stallLLM{}, Config{Permission: "allow", MaxAgents: 10})
	start := time.Now()
	text, err := a.drainText(context.Background(), AgentSpec{}, port.ChatRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a silent stream should drain cleanly (empty), got err %v", err)
	}
	if text != "" {
		t.Errorf("a silent stream yields no text, got %q", text)
	}
	if elapsed > time.Second {
		t.Errorf("stall abort took %s; it must fire around streamStallTimeout (40ms), not hang", elapsed)
	}
}

// The happy path: drainText accumulates the reply's text and returns it.
func TestDrainTextAccumulates(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "the plan JSON"}, Config{Permission: "allow", MaxAgents: 10})
	text, err := a.drainText(context.Background(), AgentSpec{}, port.ChatRequest{})
	if err != nil || text != "the plan JSON" {
		t.Fatalf("drainText = %q, %v; want \"the plan JSON\", nil", text, err)
	}
}
