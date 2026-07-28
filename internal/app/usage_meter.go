package app

import (
	"context"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// What a run actually spends was not knowable from anything magi recorded.
//
// The turn's usage came from ONE place — the main agent's own stream — and everything else billed
// against the same account was invisible: the council's members (three of them, per round, per gate,
// and there are several gates in a turn), the curator, both spec-mine passes, the check audit, the
// coverage fill and its re-ask, the convergence judge, the lease judge. A delegate-heavy turn was
// worse still: the work happens in a child session, so the parent's own numbers approach zero while
// the run costs the most.
//
// And the one number that WAS recorded means something other than its name. `Usage.In` is the LAST
// step's prompt size — deliberately, because the context meter wants the current window — while
// `Usage.Cost` sums every step's prompt. Read `In` as "input tokens this turn" and a twenty-step
// turn under-reports by a factor of twenty; the cost beside it is right, which makes the mismatch
// easy to miss.
//
// So totals are collected at the one seam every request passes through: the provider. A wrapper tees
// each stream's usage event into a ledger before handing it on, which means a call site added later
// is counted without anyone remembering to count it. Attribution is by session, taken from the
// context — but the GRAND TOTAL is incremented unconditionally, so a call made on a context nobody
// tagged still lands in the number this exists to produce.

// usageSIDKey carries the session a request is being made for. Set once per run (runLoop), so every
// side call and council poll beneath it inherits the attribution without its own plumbing.
type usageSIDKey struct{}

func ctxWithUsageSID(ctx context.Context, sid session.SessionID) context.Context {
	return context.WithValue(ctx, usageSIDKey{}, sid)
}

func usageSIDFrom(ctx context.Context) session.SessionID {
	if v, ok := ctx.Value(usageSIDKey{}).(session.SessionID); ok {
		return v
	}
	return ""
}

// MeterProvider wraps a provider so every request it serves is counted. Idempotent: wrapping an
// already-metered provider returns it unchanged, so a call site that meters defensively cannot
// double-count.
//
// Exported for the one provider the App does not own: the council resolves its own backends, and
// until they were wrapped its polls — several per round, per gate — appeared in no total at all.
// Takes no lock, so providerFor can call it while holding a.mu.
func (a *App) MeterProvider(p port.LLMProvider) port.LLMProvider {
	if p == nil {
		return nil
	}
	if _, done := p.(*meteredProvider); done {
		return p
	}
	return &meteredProvider{inner: p, a: a}
}

type meteredProvider struct {
	inner port.LLMProvider
	a     *App
}

// Unwrap returns the provider underneath, so a caller that needs the backend ITSELF — an identity
// comparison, or an optional capability this wrapper does not forward — can reach it. unwrapProvider
// is the way to ask without knowing whether a wrapper is there.
func (m *meteredProvider) Unwrap() port.LLMProvider { return m.inner }

// unwrapProvider strips any metering wrapper. A provider that was never wrapped is returned as-is.
func unwrapProvider(p port.LLMProvider) port.LLMProvider {
	for {
		u, ok := p.(interface{ Unwrap() port.LLMProvider })
		if !ok {
			return p
		}
		p = u.Unwrap()
	}
}

// StreamChat forwards the request and tees the usage event out of the reply. The stream is passed
// through unchanged and in order — this observes, it never withholds or reorders — so a consumer
// that already reads usage (the main loop's context meter) is unaffected.
func (m *meteredProvider) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	in, err := m.inner.StreamChat(ctx, r)
	if err != nil {
		return nil, err
	}
	sid, model := usageSIDFrom(ctx), r.Model
	out := make(chan port.ProviderEvent)
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Type == port.ProviderUsage && ev.Usage != nil {
				m.a.recordUsage(sid, model, *ev.Usage)
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				// Drain rather than return: the inner stream's producer may block on an unread
				// channel, and leaking that goroutine leaks the request behind it.
				for range in {
				}
				return
			}
		}
	}()
	return out, nil
}

// recordUsage adds one request's tokens to the grand total and, when the context named one, to that
// session's line. Cost is priced per REQUEST — which is what a backend charges — so it sums the
// prompt of every call rather than the last one's.
func (a *App) recordUsage(sid session.SessionID, model string, r event.Usage) {
	if r.In <= 0 && r.Out <= 0 {
		return
	}
	r.Cost = a.cfg.Models.Get(model).Cost(r.In, r.Out)
	add := func(dst *event.Usage) {
		dst.In += r.In
		dst.Out += r.Out
		dst.Cost += r.Cost
	}
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	add(&a.usageTotal)
	if sid == "" {
		return
	}
	if a.usageBySession == nil {
		a.usageBySession = map[session.SessionID]event.Usage{}
	}
	u := a.usageBySession[sid]
	add(&u)
	a.usageBySession[sid] = u
}

// UsageTotal reports every token this process has spent, on any session or none. This is the
// billing number: it counts each request's prompt, not the last one's.
func (a *App) UsageTotal() event.Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return a.usageTotal
}

// UsageFor reports what one session cost.
func (a *App) UsageFor(sid session.SessionID) event.Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return a.usageBySession[sid]
}
