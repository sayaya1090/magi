package app

import (
	"context"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// plainLLM is a backend with nothing optional on it: it streams, and that is all. The shape a
// second adapter arrives in before anyone teaches it to be pointed somewhere else — and the shape
// UseBackend's refusal was written for.
type plainLLM struct{ fakeLLM }

// wholeRedirectLLM implements the WHOLE of port.BaseRedirector, which is the shape the wrappers
// forward: the concrete *openai.Client answers all three, and a double that answers only the
// setter is dropped by the wrapper before UseBackend ever sees it.
//
// Locked, like the real one: the window probe reads BaseURL from a goroutine while a switch writes
// it, so a double that skipped the lock the *openai.Client keeps would report a race of its own
// making — and a race detector that is busy pointing at the double is not looking at the code.
type wholeRedirectLLM struct {
	fakeLLM
	models []string

	mu   sync.Mutex
	base string
	tok  uint64
}

func (l *wholeRedirectLLM) ListModels(context.Context) ([]string, error) { return l.models, nil }

func (l *wholeRedirectLLM) SetBaseURL(b string) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.base = b
	l.tok++
	return l.tok
}

func (l *wholeRedirectLLM) ClearBaseURL(uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.base = ""
}

func (l *wholeRedirectLLM) BaseURL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.base
}

// A backend that cannot be pointed anywhere is refused THROUGH ITS WRAPPERS.
//
// The refusal existed and could not be reached. The provider an App holds is always wrapped — the
// hang guard, and on the metered path the usage meter over that — and a wrapper carries every
// optional method whether or not what it wraps does, forwarding to nothing. So the type assertion
// that asked "can this be redirected" met the wrapper and said yes on every real build; UseBackend
// returned nil and the console reported a backend switch that had not happened. Only a bare
// double, which no production path constructs, ever took the refusing branch.
//
// What answers the question is the token: a real one is never 0, and a wrapper with nothing to
// forward to returns 0.
func TestABackendThatCannotBeRedirectedIsRefusedThroughItsWrappers(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(port.LLMProvider) port.LLMProvider
	}{
		{"bare", func(p port.LLMProvider) port.LLMProvider { return p }},
		{"guarded", GuardProvider},
		{"guarded and metered", func(p port.LLMProvider) port.LLMProvider {
			return (&App{}).MeterProvider(GuardProvider(p))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWith(t, tc.wrap(&plainLLM{}))
			if err := a.UseBackend(open(t, a), "http://127.0.0.1:9/v1"); err == nil {
				t.Fatal("the switch was reported as done; nothing was redirected")
			}
		})
	}
}

// And a backend that CAN be redirected still is, through the same wrappers — the refusal must not
// be bought by breaking the control it guards.
func TestARedirectableBackendStillSwitchesThroughItsWrappers(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(port.LLMProvider) port.LLMProvider
	}{
		{"guarded", GuardProvider},
		{"guarded and metered", func(p port.LLMProvider) port.LLMProvider {
			return (&App{}).MeterProvider(GuardProvider(p))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := &wholeRedirectLLM{models: []string{"alpha-default"}}
			a := newAppWith(t, tc.wrap(llm))
			sid := open(t, a)
			if err := a.UseBackend(sid, "http://127.0.0.1:9/v1"); err != nil {
				t.Fatalf("UseBackend through the wrappers: %v", err)
			}
			if llm.base != "http://127.0.0.1:9/v1" {
				t.Errorf("the wrapped provider was not redirected: %q", llm.base)
			}
			if got := modelOf(t, a, sid); got != "alpha-default" {
				t.Errorf("model = %q; the switch did not carry through to the served model", got)
			}
		})
	}
}

// The wrappers answer 0 for an inner that cannot redirect, and pass a real token straight back.
// This is the contract UseBackend reads, stated where a future wrapper will be compared to it: a
// wrapper that invents a token puts the unreachable refusal back.
func TestAWrapperAnswersZeroForABackendItCannotRedirect(t *testing.T) {
	if tok := GuardProvider(&plainLLM{}).(port.BaseRedirector).SetBaseURL("http://x"); tok != 0 {
		t.Errorf("the guard invented a token %d for a backend that redirects nothing", tok)
	}
	// Metering a BARE provider, not the guard: over the guard the meter forwards to something
	// that is itself a redirector, so its own answer for "nothing to forward to" never runs.
	if tok := (&App{}).MeterProvider(&plainLLM{}).(port.BaseRedirector).SetBaseURL("http://x"); tok != 0 {
		t.Errorf("the meter invented a token %d for a backend that redirects nothing", tok)
	}
	if tok := GuardProvider(&wholeRedirectLLM{}).(port.BaseRedirector).SetBaseURL("http://x"); tok == 0 {
		t.Error("the guard reported nothing redirected for a backend that redirected")
	}
}
