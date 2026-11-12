package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// gateCooldownBase / gateCooldownMax bound the shared cooldown a transient failure without a
// server-named wait earns: base, doubled per consecutive failure, capped. A named Retry-After wins.
var (
	gateCooldownBase = time.Second
	gateCooldownMax  = 60 * time.Second
)

// gateOpenAfter consecutive transient failures open the circuit: once the cooldown is over, ONE
// probe request goes through and every other caller is refused until it answers.
const gateOpenAfter = 3

// backendGates is the state every caller of one backend shares \u2014 the main step, the council, the
// summarizer \u2014 keyed by (endpoint, model). It exists so a 429 one of them earned is honoured by all
// of them: none asks again before the backend said to, and a backend that keeps failing is probed by
// one request rather than hammered by all. Process-wide (one App), not across processes. The zero
// value is ready to use; now is injectable for tests.
type backendGates struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*gateState
	// seq numbers every recorded transient failure. A ticket remembers the number it was admitted
	// under, so an answer to a request that was already in flight when a newer refusal arrived does
	// not clear that refusal: it says how the backend was BEFORE, not after.
	seq uint64
}

type gateState struct {
	until    time.Time
	kind     port.BackendErrorKind
	failures int
	probing  bool
	gen      uint64 // seq of the last failure recorded here
}

// gateTicket is one admitted request; done reports how it went. A zero ticket's done is a no-op.
type gateTicket struct {
	g     *backendGates
	key   string
	probe bool
	seen  uint64 // g.seq when admitted
}

func (g *backendGates) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// admit lets a request through, or refuses it WITHOUT calling the backend: a retryable
// port.BackendError naming the remaining wait, so the main step sits it out and a side call (council,
// summarizer) takes its existing failure path \u2014 a silent member, a history left unfolded.
func (g *backendGates) admit(key string) (gateTicket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.m[key]
	if s == nil {
		return gateTicket{g: g, key: key, seen: g.seq}, nil
	}
	if now := g.clock(); now.Before(s.until) {
		return gateTicket{}, gateRefusal(s, s.until.Sub(now), "the backend is cooling down after a refusal another request got")
	}
	if s.failures >= gateOpenAfter {
		if s.probing {
			return gateTicket{}, gateRefusal(s, gateCooldownBase, "the backend kept failing; one probe request is out and the rest wait for its answer")
		}
		s.probing = true
		return gateTicket{g: g, key: key, probe: true, seen: g.seq}, nil
	}
	return gateTicket{g: g, key: key, seen: g.seq}, nil
}

// errGateHeld marks a refusal the gate made itself: the backend was NOT called. The main step waits
// it out against its wait budget but does not count it as an attempt — otherwise a cooldown another
// caller earned would spend this step's attempts without one request reaching the backend.
var errGateHeld = errors.New("backend not called")

func isGateHeld(err error) bool { return errors.Is(err, errGateHeld) }

func gateRefusal(s *gateState, wait time.Duration, why string) error {
	kind := s.kind
	if kind == "" {
		kind = port.KindOverloaded
	}
	return &port.BackendError{Kind: kind, RetryAfter: wait,
		Cause: fmt.Errorf("%w: %s (%d consecutive failures)", errGateHeld, why, s.failures)}
}

// done records how an admitted request went. nil, or a failure waiting cannot fix (the backend did
// answer), clears the state; a retryable failure extends the shared cooldown and counts toward the
// circuit; a cancellation is the caller's, says nothing about the backend, and only frees the probe.
func (t gateTicket) done(err error) {
	g := t.g
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.m[t.key]
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		if s != nil && t.probe {
			s.probing = false
		}
		return
	}
	cls := classifyBackend(err)
	if err == nil || !cls.Kind.Retryable() {
		if s != nil && s.gen > t.seen {
			// a failure was recorded after this request went out: its answer is older news
			if t.probe {
				s.probing = false
			}
			return
		}
		delete(g.m, t.key)
		return
	}
	if s == nil {
		if g.m == nil {
			g.m = map[string]*gateState{}
		}
		s = &gateState{}
		g.m[t.key] = s
	}
	g.seq++
	s.gen = g.seq
	s.failures++
	s.probing = false
	s.kind = cls.Kind
	wait := cls.RetryAfter
	if wait <= 0 {
		wait = gateCooldownBase
		for i := 1; i < s.failures && wait < gateCooldownMax; i++ {
			wait *= 2
		}
		if wait > gateCooldownMax {
			wait = gateCooldownMax
		}
	}
	if until := g.clock().Add(wait); until.After(s.until) {
		s.until = until
	}
}
