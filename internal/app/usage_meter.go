package app

import (
	"context"
	"sync"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// What a run actually spends was not knowable from anything magi recorded.
//
// The turn's usage came from ONE place — the main agent's own stream — and everything else billed
// against the same account was invisible: the council's members — three of them, per round, per
// gate, and there are several gates in a turn. (This list used to also name a curator, two
// spec-mine passes, a check audit, a coverage fill and its re-ask, a convergence judge and a lease
// judge. None of those exist any more; the meter counting them is what is left.)
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
// comparison, say — can reach it.
//
// It is no longer the way to reach an optional capability: this wrapper forwards them (below), and
// a caller that had to unwrap to ask a question was a caller who could forget to.
func (m *meteredProvider) Unwrap() port.LLMProvider { return m.inner }

// Every optional capability, forwarded. This wrapper sits OUTSIDE the guard on every model request
// (providerFor), so a capability it drops is one no caller downstream can reach — the same silent
// loss the guard was found guilty of, one layer further out.
var _ port.ProviderExtras = (*meteredProvider)(nil)

func (m *meteredProvider) ListModels(ctx context.Context) ([]string, error) {
	lister, ok := m.inner.(port.ModelLister)
	if !ok {
		return nil, port.ErrCapabilityAbsent
	}
	return lister.ListModels(ctx)
}

func (m *meteredProvider) ProbeContextWindow(ctx context.Context, model string) (int, bool) {
	prober, ok := m.inner.(port.ContextProber)
	if !ok {
		return 0, false
	}
	return prober.ProbeContextWindow(ctx, model)
}

func (m *meteredProvider) SetBaseURL(url string) uint64 {
	setter, ok := m.inner.(port.BaseRedirector)
	if !ok {
		return 0
	}
	return setter.SetBaseURL(url)
}

func (m *meteredProvider) ClearBaseURL(tok uint64) {
	if clearer, ok := m.inner.(port.BaseRedirector); ok {
		clearer.ClearBaseURL(tok)
	}
}

func (m *meteredProvider) BaseURL() string {
	if reader, ok := m.inner.(port.BaseRedirector); ok {
		return reader.BaseURL()
	}
	return ""
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
	r.Cost = a.cfg.Models.Get(model).Cost(r.In, r.Out)
	a.usage.record(sid, r)
}

// usageLedger is every token this process has spent, by session and in total. It owns its own lock
// because recordUsage runs on every model goroutine and must never queue behind the state lock —
// which is also why it is a type rather than three more fields on App: the invariant "these three
// are read and written together, under THIS mutex" is stated once here instead of being a rule
// every future call site has to remember.
type usageLedger struct {
	mu        sync.Mutex
	total     event.Usage
	bySession map[session.SessionID]event.Usage
}

func addUsage(dst *event.Usage, r event.Usage) {
	dst.In += r.In
	dst.Out += r.Out
	dst.Cost += r.Cost
	dst.Cached += r.Cached
	// Sticky, unlike the counts. "Did this backend ever tell us about its cache" is a fact about
	// the backend, and one response that omitted the details block does not unsay it.
	dst.CacheReported = dst.CacheReported || r.CacheReported
}

// record adds one request to the grand total and, when the context named one, to that session's
// line. The total is unconditional: a call made on a context nobody tagged still belongs on the
// bill this exists to produce.
func (l *usageLedger) record(sid session.SessionID, r event.Usage) {
	if r.In <= 0 && r.Out <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	addUsage(&l.total, r)
	if sid == "" {
		return
	}
	if l.bySession == nil {
		l.bySession = map[session.SessionID]event.Usage{}
	}
	u := l.bySession[sid]
	addUsage(&u, r)
	l.bySession[sid] = u
}

func (l *usageLedger) grandTotal() event.Usage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.total
}

func (l *usageLedger) forSession(sid session.SessionID) event.Usage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bySession[sid]
}

// forget drops a session's per-session line. The grand total is unaffected — record() already added
// this session's spend to it unconditionally, and the total is the bill. Called when a spawned child
// ends: its per-session cost was reported while it ran, and nothing reads a finished child's line
// (UsageFor serves the session being run or the one a human is attached to), so keeping it only grew
// bySession by one dead entry per child for the process lifetime.
func (l *usageLedger) forget(sid session.SessionID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.bySession, sid)
}

// UsageTotal reports every token this process has spent, on any session or none. This is the
// billing number: it counts each request's prompt, not the last one's.
func (a *App) UsageTotal() event.Usage { return a.usage.grandTotal() }

// UsageFor reports what one session cost.
func (a *App) UsageFor(sid session.SessionID) event.Usage { return a.usage.forSession(sid) }
