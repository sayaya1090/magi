package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/port"
)

type gateClock struct{ t time.Time }

func (c *gateClock) now() time.Time          { return c.t }
func (c *gateClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func rateLimited(after time.Duration) error {
	return &port.BackendError{Kind: port.KindRateLimit, Status: 429, RetryAfter: after, Cause: errors.New("llm: rate limited (status 429)")}
}

func overloaded() error {
	return &port.BackendError{Kind: port.KindOverloaded, Status: 529, Cause: errors.New("overloaded")}
}

// One caller's 429 is every caller's: the main step, the council and the summarizer share the
// backend, so none of them asks again before the backend said to. Another endpoint is not held up.
func TestGateSharesACooldownAcrossCallers(t *testing.T) {
	clk := &gateClock{t: time.Unix(1_000_000, 0)}
	g := &backendGates{now: clk.now}
	tk, err := g.admit("a|m")
	if err != nil {
		t.Fatal(err)
	}
	tk.done(rateLimited(30 * time.Second))
	_, err = g.admit("a|m")
	be, ok := port.AsBackendError(err)
	if !ok || be.Kind != port.KindRateLimit || be.RetryAfter <= 0 || be.RetryAfter > 30*time.Second {
		t.Fatalf("want a shared rate_limit refusal naming the remaining wait, got %v", err)
	}
	if _, err := g.admit("b|m"); err != nil {
		t.Errorf("another endpoint must not wait on this one: %v", err)
	}
	clk.advance(31 * time.Second)
	if _, err := g.admit("a|m"); err != nil {
		t.Errorf("the cooldown should be over: %v", err)
	}
}

// Repeated failures open the circuit; once its wait is over ONE probe goes through while the
// others are still refused, and a success closes it again.
func TestGateOpensAfterRepeatedFailuresAndLetsOneProbeThrough(t *testing.T) {
	clk := &gateClock{t: time.Unix(1_000_000, 0)}
	g := &backendGates{now: clk.now}
	for i := 0; i < gateOpenAfter; i++ {
		clk.advance(time.Hour)
		tk, err := g.admit("k")
		if err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
		tk.done(overloaded())
	}
	clk.advance(time.Hour)
	probe, err := g.admit("k")
	if err != nil {
		t.Fatalf("the probe should be let through: %v", err)
	}
	if _, err := g.admit("k"); err == nil {
		t.Fatal("a second request while the probe is out must be refused")
	}
	probe.done(nil)
	for i := 0; i < 3; i++ {
		tk, err := g.admit("k")
		if err != nil {
			t.Fatalf("after a good probe the circuit should be closed: %v", err)
		}
		tk.done(nil)
	}
}

// A stop the person pressed, or a failure waiting cannot fix, says nothing about the backend's load.
func TestGateIgnoresCancelAndPermanentFailures(t *testing.T) {
	clk := &gateClock{t: time.Unix(1_000_000, 0)}
	g := &backendGates{now: clk.now}
	for _, e := range []error{context.Canceled, &port.BackendError{Kind: port.KindAuth, Status: 401, Cause: errors.New("bad key")}} {
		for i := 0; i < gateOpenAfter+1; i++ {
			tk, err := g.admit("k")
			if err != nil {
				t.Fatalf("%v must not close the gate: %v", e, err)
			}
			tk.done(e)
		}
	}
}

type cooldownLLM struct {
	mu     sync.Mutex
	calls  int
	reqErr error
	frame  error
}

func (l *cooldownLLM) count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.calls }

func (l *cooldownLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	l.mu.Lock()
	l.calls++
	l.mu.Unlock()
	if l.reqErr != nil {
		return nil, l.reqErr
	}
	ch := make(chan port.ProviderEvent, 2)
	if l.frame != nil {
		ch <- port.ProviderEvent{Type: port.ProviderError, Err: l.frame}
	} else {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	}
	close(ch)
	return ch, nil
}

// The gate sits where every model request passes (providerFor's wrapper), so two callers that
// each fetched the provider \u2014 a step and a council member, say \u2014 see the same cooldown, whether the
// refusal came on the request or as an error frame in the stream (how every plugin reports one).
func TestCooldownIsSharedByEveryCallerOfAProvider(t *testing.T) {
	for name, llm := range map[string]*cooldownLLM{
		"request": {reqErr: rateLimited(time.Hour)},
		"frame":   {frame: rateLimited(time.Hour)},
	} {
		store, _ := jsonl.New(t.TempDir())
		a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
		ctx := context.Background()
		if ch, err := a.providerFor(AgentSpec{}).StreamChat(ctx, port.ChatRequest{Model: "m"}); err == nil {
			for range ch {
			}
		}
		_, err := a.providerFor(AgentSpec{}).StreamChat(ctx, port.ChatRequest{Model: "m"})
		if be, ok := port.AsBackendError(err); !ok || be.Kind != port.KindRateLimit {
			t.Errorf("%s: the second caller should be refused by the shared cooldown, got %v", name, err)
		}
		if c := llm.count(); c != 1 {
			t.Errorf("%s: the backend should have been called once, got %d", name, c)
		}
	}
}

// A request admitted BEFORE another caller earned a cooldown says nothing about the backend after
// that refusal: its success must not clear the newer Retry-After. Reported against 4c5c26bc.
func TestAStaleSuccessDoesNotClearANewerCooldown(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	g := &backendGates{now: func() time.Time { return now }}
	a, err := g.admit("k")
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.admit("k")
	if err != nil {
		t.Fatal(err)
	}
	a.done(&port.BackendError{Kind: port.KindRateLimit, Status: 429, RetryAfter: 60 * time.Second, Cause: errors.New("429")})
	b.done(nil)
	now = now.Add(10 * time.Second)
	if _, err := g.admit("k"); err == nil {
		t.Fatal("a success admitted before the 429 cleared its 60s cooldown; the next request went out after 10s")
	}
	// the same for a permanent failure that was already in flight
	c, _ := g.admit("j")
	d, _ := g.admit("j")
	c.done(&port.BackendError{Kind: port.KindRateLimit, Status: 429, RetryAfter: 60 * time.Second, Cause: errors.New("429")})
	d.done(&port.BackendError{Kind: port.KindInvalidRequest, Status: 400, Cause: errors.New("400")})
	if _, err := g.admit("j"); err == nil {
		t.Fatal("a permanent failure admitted before the 429 cleared its cooldown")
	}
	// a success admitted AFTER the cooldown still clears it
	now = now.Add(2 * time.Minute)
	e, err := g.admit("k")
	if err != nil {
		t.Fatal(err)
	}
	e.done(nil)
	g.mu.Lock()
	_, left := g.m["k"]
	g.mu.Unlock()
	if left {
		t.Fatal("a success admitted after the refusal should reset the gate")
	}
}
