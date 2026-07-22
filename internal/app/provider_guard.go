package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// guardedProvider wraps a provider so EVERY model request — the main loop, the planner, the council,
// and every tool-free side call — is protected from a hung generation at the single point the reply is
// received, instead of each consumer carrying its own watchdog (the whack-a-mole this replaces). Two
// failure modes are caught: a SILENT backend (no event for the idle bound) and a THOUGHT SPIN
// (reasoning streamed forever without ever converging to an answer). Both cancel the request and close
// the stream, so the caller unwinds in seconds instead of the turn's wall clock.
//
// The bounds are a SAFETY NET set ABOVE the main loop's own behavioural guards (consumeStream's
// stall-retry and reasoningSpinCap), so those still fire first for the main generate — preserving their
// retry/nudge behaviour — and this only backstops the guard-less paths (planner, council, side calls).
// When it fires it logs to stderr so a run can confirm the guard actually activated.
type guardedProvider struct{ inner port.LLMProvider }

// GuardProvider wraps p with the universal hang guard. Idempotent and nil-safe, so it is cheap to apply
// at every provider-creation site.
func GuardProvider(p port.LLMProvider) port.LLMProvider {
	if p == nil {
		return nil
	}
	if _, ok := p.(guardedProvider); ok {
		return p // already guarded
	}
	return guardedProvider{inner: p}
}

// providerGuardIdle / providerGuardCap are the safety-net bounds — deliberately ABOVE the main loop's
// streamStallTimeout / reasoningSpinCap so consumeStream's behavioural handling fires first for the main
// generate; the guard then only catches paths with no handling of their own. 0 disables the arm.
func providerGuardIdle() time.Duration {
	if streamStallTimeout <= 0 {
		return 0
	}
	return 2 * streamStallTimeout
}

func providerGuardCap() int {
	c := reasoningSpinCap()
	if c <= 0 {
		return 0
	}
	return 2 * c
}

func (g guardedProvider) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	gctx, cancel := context.WithCancel(ctx)
	inner, err := g.inner.StreamChat(gctx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan port.ProviderEvent)
	go func() {
		defer close(out)
		defer cancel()
		idle := providerGuardIdle()
		byteCap := providerGuardCap()
		streamed := 0 // reasoning+text bytes, for the spin backstop
		last := time.Now()
		tick := 15 * time.Second
		if idle > 0 && idle < tick {
			tick = idle
		}
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case ev, ok := <-inner:
				if !ok {
					return
				}
				last = time.Now()
				streamed += len(ev.Text) // Text carries both ProviderText and ProviderReasoning content
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
				if byteCap > 0 && streamed > byteCap {
					fmt.Fprintf(os.Stderr, "magi: stream-guard aborted a runaway generation (%d bytes, no completion — likely a reasoning spin)\n", streamed)
					cancel()
				}
			case now := <-t.C:
				if idle > 0 && now.Sub(last) >= idle {
					fmt.Fprintf(os.Stderr, "magi: stream-guard aborted a silent stream (no data for %s — hung backend)\n", now.Sub(last).Round(time.Second))
					cancel()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
