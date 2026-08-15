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
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow"})
	// A stream with NO output is bounded by the first-token wait now, so shrink that one; keep the
	// inter-token bound small too so neither strands the test.
	oldS, oldF := streamStallTimeout, firstTokenTimeout
	streamStallTimeout, firstTokenTimeout = 40*time.Millisecond, 40*time.Millisecond
	defer func() { streamStallTimeout, firstTokenTimeout = oldS, oldF }()

	stream := make(chan port.ProviderEvent) // never sends → perpetually silent
	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) } // mirrors streamCtx cancel unblocking the provider read

	start := time.Now()
	res, err := a.consumeStream(context.Background(), session.SessionID("s_stall"), event.Actor{}, stream, "m", cancel)
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

// A stream that stays silent LONGER than the inter-token bound but shorter than the first-token
// bound, then emits — the PREFILL case — must NOT be aborted or marked stalled. No first token yet is
// "still prefilling", not a hang: a big prompt on a slow local backend legitimately takes minutes to
// its first token. This is the Qwen3.8-on-an-M4-Pro turn the old single-bound watchdog killed.
func TestConsumeStreamToleratesASlowFirstTokenPrefill(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow"})
	oldS, oldF := streamStallTimeout, firstTokenTimeout
	streamStallTimeout = 30 * time.Millisecond // inter-token bound: short
	firstTokenTimeout = 5 * time.Second        // first-token bound: generous
	defer func() { streamStallTimeout, firstTokenTimeout = oldS, oldF }()

	stream := make(chan port.ProviderEvent, 2)
	go func() {
		time.Sleep(120 * time.Millisecond) // silent for 4x the inter-token bound: a "prefill"
		stream <- port.ProviderEvent{Type: port.ProviderText, Text: "hello"}
		stream <- port.ProviderEvent{Type: port.ProviderFinish}
		close(stream)
	}()
	var cancelled atomic.Bool
	res, err := a.consumeStream(context.Background(), session.SessionID("s_prefill"), event.Actor{}, stream, "m",
		func() { cancelled.Store(true) })
	if err != nil {
		t.Fatalf("a slow first token must not error: %v", err)
	}
	if res.stalled {
		t.Error("a slow prefill (first token late) must NOT be marked stalled — it was alive, just prefilling")
	}
	if cancelled.Load() {
		t.Error("the stream was cancelled during prefill — the first-token bound was not honoured")
	}
	if res.text != "hello" {
		t.Errorf("the output after the slow first token was lost: %q", res.text)
	}
}

// A stream that emits a token and THEN goes silent is aborted just the same (the turn can't hang),
// but it is NOT marked stalled: output was already committed, so re-issuing the request would
// double-generate. The caller finishes with the partial output instead of retrying.
func TestConsumeStreamMidGenerationFreezeNotRetryable(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow"})
	old := streamStallTimeout
	streamStallTimeout = 40 * time.Millisecond
	defer func() { streamStallTimeout = old }()

	stream := make(chan port.ProviderEvent, 1)
	stream <- port.ProviderEvent{Type: port.ProviderText, Text: "partial"}
	// then never closed and never sent again → freezes mid-generation

	res, err := a.consumeStream(context.Background(), session.SessionID("s_freeze"), event.Actor{}, stream, "m", func() {})
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

// The provider guard is the safety net UNDER consumeStream's handling, so its idle bound must sit
// ABOVE both of the main loop's silence bounds — above the inter-token one AND the first-token one.
// Sized from streamStallTimeout alone it sat at 240s, below the 300s first-token bound: a slow
// prefill was killed by the guard at 240s instead of handled at 300s, and killed in the worst way —
// the guard's cancel closes the stream without the idle tick firing, so `stalled` is never set, the
// retry ladder is unreachable, and the turn ends as an error-free empty answer.
func TestProviderGuardIdleSitsAboveBothStreamBounds(t *testing.T) {
	oldS, oldF := streamStallTimeout, firstTokenTimeout
	defer func() { streamStallTimeout, firstTokenTimeout = oldS, oldF }()

	streamStallTimeout, firstTokenTimeout = 120*time.Second, 300*time.Second
	if got := providerGuardIdle(); got <= firstTokenBound() {
		t.Errorf("guard idle %v is not above the first-token bound %v — a slow prefill dies to the "+
			"guard before consumeStream can handle it", got, firstTokenBound())
	}
	streamStallTimeout, firstTokenTimeout = 120*time.Second, 0 // no separate first-token bound
	if got := providerGuardIdle(); got <= streamStallTimeout {
		t.Errorf("guard idle %v is not above the inter-token bound %v", got, streamStallTimeout)
	}
	streamStallTimeout, firstTokenTimeout = 0, 0 // everything off
	if got := providerGuardIdle(); got != 0 {
		t.Errorf("guard idle %v with both bounds disabled — the old fully-off behaviour is gone", got)
	}
}
