package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// A backend that is busy says so in many dialects: the OpenAI adapter's status line, a plugin's
// error frame carrying the Anthropic body, a gateway's 503. Each must be read as "ask again
// later" — and an auth failure, a size refusal, an exhausted quota or a cut stream must not.
func TestIsTransientBackend(t *testing.T) {
	hits := []error{
		errors.New("llm: rate limited (status 429): {\"error\":\"slow down\"}"),
		errors.New("llm: gateway unavailable — the backend/model is down or overloaded (status 503): "),
		errors.New("llm: server error (status 500): oops"),
		errors.New(`provider stream: API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
		errors.New("provider stream: Overloaded"),
		errors.New("provider stream: 429 Too Many Requests"),
		errors.New(`provider stream: {"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}`),
		errors.New("provider stream: The service is temporarily unavailable"),
	}
	for _, e := range hits {
		if !isTransientBackend(e) {
			t.Errorf("should be transient: %v", e)
		}
	}
	misses := []error{
		nil,
		errors.New("llm: unauthorized — check the API key (MAGI_API_KEY) (status 401): bad key"),
		errors.New("prompt is too long: 210000 tokens > 200000 maximum"),
		errors.New("llm: rate limited (status 429): You exceeded your current quota, please check your plan and billing details. insufficient_quota"),
		errors.New("provider stream: Claude AI usage limit reached|1762963200"),
		fmt.Errorf("%w: no finish_reason", port.ErrStreamCut),
		fmt.Errorf("%w: silent", port.ErrStreamAborted),
	}
	for _, e := range misses {
		if isTransientBackend(e) {
			t.Errorf("should NOT be transient: %v", e)
		}
	}
}

func TestRetryAfterHint(t *testing.T) {
	if d := retryAfterHint(errors.New("llm: rate limited (status 429, retry after 30s): x")); d != 30*time.Second {
		t.Errorf("want 30s, got %v", d)
	}
	if d := retryAfterHint(errors.New("llm: rate limited (status 429): x")); d != 0 {
		t.Errorf("no hint: want 0, got %v", d)
	}
}

// The ladder grows, never passes its ceiling, is jittered (two attempts at the same step do not all
// land on the same instant), and a server's own Retry-After wins over the guess — up to a cap.
func TestTransientDelay(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		full := transientBackoffBase << attempt
		if full > transientBackoffMax || full <= 0 {
			full = transientBackoffMax
		}
		for i := 0; i < 50; i++ {
			d := transientDelay(attempt, 0)
			if d < full/2 || d > full {
				t.Fatalf("attempt %d: delay %v outside [%v, %v]", attempt, d, full/2, full)
			}
		}
	}
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[transientDelay(3, 0)] = true
	}
	if len(seen) < 2 {
		t.Error("delay is not jittered")
	}
	if d := transientDelay(0, 17*time.Second); d != 17*time.Second {
		t.Errorf("Retry-After should win: got %v", d)
	}
	// Never shortened: the budget check in generateStep, not this, bounds a long named wait.
	if d := transientDelay(0, time.Hour); d != time.Hour {
		t.Errorf("Retry-After must not be shortened, got %v", d)
	}
}

func TestSleepCtxStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if sleepCtx(ctx, time.Hour) {
		t.Error("a cancelled wait must report false")
	}
	if time.Since(start) > time.Second {
		t.Error("a cancelled wait must return at once")
	}
}

// flakyLLM fails its first failN calls with msg — as a request error (inStream false) or as an error
// inside an otherwise accepted stream (inStream true, the shape a plugin backend uses) — then answers.
type flakyLLM struct {
	mu       sync.Mutex
	calls    int
	failN    int
	inStream bool
	msg      string
	err      error // when set, failed with this instead of msg (a structured port.BackendError)
}

func (f *flakyLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	if n <= f.failN {
		fail := errors.New(f.msg)
		if f.err != nil {
			fail = f.err
		}
		if !f.inStream {
			return nil, fail
		}
		ch <- port.ProviderEvent{Type: port.ProviderError, Err: fail}
		close(ch)
		return ch, nil
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *flakyLLM) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func fastTransient(t *testing.T) {
	b, m := transientBackoffBase, transientBackoffMax
	transientBackoffBase, transientBackoffMax = time.Millisecond, 4*time.Millisecond
	gb, gm := gateCooldownBase, gateCooldownMax
	gateCooldownBase, gateCooldownMax = time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() {
		transientBackoffBase, transientBackoffMax = b, m
		gateCooldownBase, gateCooldownMax = gb, gm
	})
}

func runFlaky(t *testing.T, llm *flakyLLM) []event.Event {
	t.Helper()
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	return runToTerminal(t, a, sid)
}

func retryNotes(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == event.TypeToolProgress && strings.Contains(string(e.Data), "retrying in") {
			n++
		}
	}
	return n
}

// A 429 that outlasts the adapter's own short retries no longer ends the turn: the step waits and
// asks again, says that it is doing so, and the turn finishes with no error recorded.
func TestRequestRateLimitIsRetriedAcrossTheStep(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 2, msg: "llm: rate limited (status 429): slow down"}
	got := runFlaky(t, llm)
	fmt.Printf("TRANSIENT request calls=%d errors=%d notes=%d\n", llm.callCount(), countType(got, event.TypeError), retryNotes(got))
	if n := countType(got, event.TypeError); n != 0 {
		t.Fatalf("429 should have been retried, got %d error(s); types=%v", n, typesOf(got))
	}
	if !lastData(got, event.TypeTurnFinished, &event.TurnFinishedData{}) {
		t.Fatal("turn did not finish after the retries")
	}
	if c := llm.callCount(); c < 3 {
		t.Errorf("want >=3 calls (two refused, one answered), got %d", c)
	}
	if retryNotes(got) < 2 {
		t.Errorf("each wait should be announced; notes=%d types=%v", retryNotes(got), typesOf(got))
	}
}

// The plugin shape: the request is accepted and the stream then carries the overload, before any
// output. Same recovery.
func TestInStreamOverloadIsRetriedAcrossTheStep(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 1, inStream: true, msg: `provider stream: API Error: 529 {"type":"overloaded_error","message":"Overloaded"}`}
	got := runFlaky(t, llm)
	fmt.Printf("TRANSIENT stream calls=%d errors=%d notes=%d\n", llm.callCount(), countType(got, event.TypeError), retryNotes(got))
	if n := countType(got, event.TypeError); n != 0 {
		t.Fatalf("in-stream overload should have been retried, got %d error(s); types=%v", n, typesOf(got))
	}
	if !lastData(got, event.TypeTurnFinished, &event.TurnFinishedData{}) {
		t.Fatal("turn did not finish after the retry")
	}
}

// A backend that stays busy still ends the turn — once, with the backend's own words, after the
// bounded ladder; not an error per attempt and not a wait without end.
func TestTransientRetriesAreBounded(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 1 << 30, msg: "llm: rate limited (status 429): slow down"}
	got := runFlaky(t, llm)
	fmt.Printf("TRANSIENT bounded calls=%d errors=%d\n", llm.callCount(), countType(got, event.TypeError))
	if n := countType(got, event.TypeError); n != 1 {
		t.Fatalf("want exactly 1 error after the ladder, got %d; types=%v", n, typesOf(got))
	}
	if c := llm.callCount(); c != maxTransientRetries+1 {
		t.Errorf("want %d calls (1 + %d retries), got %d", maxTransientRetries+1, maxTransientRetries, c)
	}
}

// A non-transient refusal is not retried at all.
func TestAuthFailureIsNotRetried(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 1 << 30, msg: "llm: unauthorized — check the API key (MAGI_API_KEY) (status 401): bad key"}
	got := runFlaky(t, llm)
	if c := llm.callCount(); c != 1 {
		t.Errorf("401 must not be retried, got %d calls", c)
	}
	if n := countType(got, event.TypeError); n != 1 {
		t.Errorf("want 1 error, got %d", n)
	}
}
