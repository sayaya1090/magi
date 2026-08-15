package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sayaya1090/magi/internal/envflag"
	"github.com/sayaya1090/magi/internal/port"
)

// Degenerate-repetition backstop bounds: the guard aborts a stream whose recent tail is a short unit
// repeated back-to-back — the "the the the…" / one-sentence-forever loop weak models fall into — far
// sooner than the raw byte cap (which only fires after ~800KB). A unit up to repMaxUnit bytes repeated
// at least repMinReps times AND covering at least repMinBlock bytes of the tail is a loop; a pure-
// whitespace unit (blank lines) is ignored. The tail is capped at repTailMax and checked every
// repCheckEvery streamed bytes so the scan stays cheap.
const (
	repTailMax    = 4096
	repCheckEvery = 256
	repMaxUnit    = 256
	repMinReps    = 3
	repMinBlock   = 128
)

// providerGuardRepeatEnabled gates the repetition backstop (MAGI_REPEAT_CAP, default on).
func providerGuardRepeatEnabled() bool { return envflag.Enabled("MAGI_REPEAT_CAP", true) }

// degenerateRepeat returns the shortest unit length the tail ENDS WITH that is repeated back-to-back
// enough to be a runaway repetition loop (0 = none). Cheap for normal text: a non-repeating tail
// mismatches at the first comparison for each candidate period.
func degenerateRepeat(tail []byte) int {
	n := len(tail)
	if n < repMinBlock {
		return 0
	}
	maxP := repMaxUnit
	if maxP > n/repMinReps {
		maxP = n / repMinReps
	}
	for p := 1; p <= maxP; p++ {
		unit := tail[n-p:]
		if len(bytes.TrimSpace(unit)) == 0 {
			continue // a run of pure whitespace/newlines is not a degenerate CONTENT loop
		}
		reps := 1
		for k := 2; k*p <= n; k++ {
			if bytes.Equal(tail[n-k*p:n-(k-1)*p], unit) {
				reps = k
			} else {
				break
			}
		}
		if reps >= repMinReps && reps*p >= repMinBlock {
			return p
		}
	}
	return 0
}

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
// bounds so consumeStream's behavioural handling fires first for the main generate; the guard then
// only catches paths with no handling of their own. 0 disables the arm.
//
// Above BOTH of the main loop's silence bounds, which came apart once: sized from streamStallTimeout
// alone (2×120s=240s), the guard sat BELOW the 300s first-token bound — so a slow prefill was killed
// at 240s by the guard instead of handled at 300s by consumeStream, and killed WORSE: the guard's
// cancel closes the stream without the idle tick ever firing, so `stalled` was never set, the retry
// ladder was unreachable, and the turn ended as an error-free empty answer. The net was the exact
// hang-fix being undone one layer up, silently.
func providerGuardIdle() time.Duration {
	inner := streamStallTimeout
	if b := firstTokenBound(); b > inner {
		inner = b
	}
	if inner <= 0 {
		return 0
	}
	return 2 * inner
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
		repCap := providerGuardRepeatEnabled()
		streamed := 0   // reasoning+text bytes, for the spin backstop
		var tail []byte // recent output, for the repetition backstop
		sinceRepCheck := 0
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
				if repCap && len(ev.Text) > 0 {
					tail = append(tail, ev.Text...)
					if len(tail) > repTailMax {
						tail = tail[len(tail)-repTailMax:]
					}
					if sinceRepCheck += len(ev.Text); sinceRepCheck >= repCheckEvery {
						sinceRepCheck = 0
						if p := degenerateRepeat(tail); p > 0 {
							fmt.Fprintf(os.Stderr, "magi: stream-guard aborted a degenerate repetition loop (a %d-byte unit repeated)\n", p)
							cancel()
						}
					}
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
