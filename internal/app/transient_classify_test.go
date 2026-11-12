package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// Structured facts win over wording: a backend that SAYS quota is not retried even when its text
// reads like a rate limit, and one that says rate_limit is retried whatever its text.
func TestClassifyPrefersStructuredFacts(t *testing.T) {
	quota := &port.BackendError{Kind: port.KindQuota, Status: 429, Cause: errors.New("rate limit exceeded")}
	if c := classifyBackend(fmt.Errorf("step: %w", quota)); c.Kind != port.KindQuota || isTransientBackend(quota) {
		t.Errorf("structured quota must win: kind=%s transient=%v", c.Kind, isTransientBackend(quota))
	}
	rl := &port.BackendError{Kind: port.KindRateLimit, RetryAfter: 42 * time.Second, RequestID: "req_1", Cause: errors.New("weird words")}
	c := classifyBackend(fmt.Errorf("wrapped: %w", rl))
	if c.Kind != port.KindRateLimit || !isTransientBackend(rl) {
		t.Errorf("structured rate_limit must be retryable: %+v", c)
	}
	if c.RetryAfter != 42*time.Second || c.RequestID != "req_1" {
		t.Errorf("facts must survive classification: %+v", c)
	}
	if d := retryAfterHint(rl); d != 42*time.Second {
		t.Errorf("structured Retry-After must be the hint, got %v", d)
	}
}

// Without structure, the wording is read into the same kinds — the fallback, not the contract.
func TestClassifyFallsBackToWording(t *testing.T) {
	cases := map[string]port.BackendErrorKind{
		"llm: rate limited (status 429): slow down":                                  port.KindRateLimit,
		`provider stream: {"error":{"type":"rate_limit_error","message":"x"}}`:       port.KindRateLimit,
		`provider stream: API Error: 529 {"type":"overloaded_error"}`:                port.KindOverloaded,
		"llm: server error (status 503): x":                                          port.KindOverloaded,
		"read tcp 127.0.0.1:1->127.0.0.1:2: read: connection reset by peer":          port.KindNetwork,
		"dial tcp 127.0.0.1:5003: connect: connection refused":                       port.KindNetwork,
		"provider stream: claude exited before answering: process exited":            port.KindNetwork,
		"provider stream: agy exited before answering: process exited":               port.KindNetwork,
		"llm: gateway timeout (status 504): x":                                       port.KindOverloaded,
		"context deadline exceeded (Client.Timeout exceeded while awaiting headers)": port.KindTimeout,
		"llm: unauthorized — check the API key (status 401): bad key":                port.KindAuth,
		"llm: forbidden (status 403): no":                                            port.KindPermission,
		"llm: bad request (status 400): unknown field":                               port.KindInvalidRequest,
		"llm: rate limited (status 429): insufficient_quota, check billing":          port.KindQuota,
		"provider stream: Claude AI usage limit reached|1762963200":                  port.KindQuota,
		"prompt is too long: 210000 tokens > 200000 maximum":                         port.KindContextOverflow,
		"something nobody anticipated":                                               port.KindUnknown,
	}
	for msg, want := range cases {
		if got := classifyBackend(errors.New(msg)).Kind; got != want {
			t.Errorf("%q: kind %s, want %s", msg, got, want)
		}
	}
	if got := classifyBackend(fmt.Errorf("stream: %w", io.ErrUnexpectedEOF)).Kind; got != port.KindNetwork {
		t.Errorf("unexpected EOF: kind %s, want network", got)
	}
	if got := classifyBackend(context.Canceled).Kind; got.Retryable() {
		t.Errorf("a cancellation is the user's, never retried: kind %s", got)
	}
}
