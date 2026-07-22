package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A stream that accepts the request, then streams NOTHING (a wedged backend) must be aborted at
// streamStallTimeout — not held until the turn's wall clock — and, since no token arrived, marked
// stalled so the caller can retry. This is the cobol-modernization 45-minute hang in miniature.
func TestConsumeStreamAbortsSilentStream(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	old := streamStallTimeout
	streamStallTimeout = 40 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	stream := make(chan port.ProviderEvent) // never sends → perpetually silent
	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) } // mirrors streamCtx cancel unblocking the provider read

	start := time.Now()
	res, err := a.consumeStream(context.Background(), session.SessionID("s_stall"), event.Actor{}, stream, "m", "pt", "pr", cancel)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a silent stream should end cleanly (caller decides to retry), got err %v", err)
	}
	if !res.stalled {
		t.Error("a stream silent past streamStallTimeout with no output must be marked stalled (retryable)")
	}
	if !cancelled.Load() {
		t.Error("the stall must cancel the stream so the provider read unwinds")
	}
	if elapsed > time.Second {
		t.Errorf("stall abort took %s; it should fire around streamStallTimeout (40ms), not hang", elapsed)
	}
}

// A stream that emits a token and THEN goes silent is aborted just the same (the turn can't hang),
// but it is NOT marked stalled: output was already committed, so re-issuing the request would
// double-generate. The caller finishes with the partial output instead of retrying.
func TestConsumeStreamMidGenerationFreezeNotRetryable(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	old := streamStallTimeout
	streamStallTimeout = 40 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	stream := make(chan port.ProviderEvent, 1)
	stream <- port.ProviderEvent{Type: port.ProviderText, Text: "partial"}
	// then never closed and never sent again → freezes mid-generation

	res, err := a.consumeStream(context.Background(), session.SessionID("s_freeze"), event.Actor{}, stream, "m", "pt", "pr", func() {})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if res.stalled {
		t.Error("a freeze AFTER a token must NOT be marked stalled (retry would double-generate)")
	}
	if res.text != "partial" {
		t.Errorf("the partial output must be preserved, got %q", res.text)
	}
}
