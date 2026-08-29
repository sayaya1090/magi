package app

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A wrapper owes the thing it wraps its WHOLE optional surface, and the obligation is checkable one
// way only if it is checked over every method and every wrapper at once.
//
// internal/arch already fails the build for a wrapper with no `var _ port.ProviderExtras = …`, but
// that assertion is satisfied by five methods that return nothing: the compiler cannot tell
// forwarding from swallowing. And the tests that did exercise behaviour named one method each —
// ListModels through both wrappers, ProbeContextWindow through the guard — which is one site
// carrying the answer for a surface of five. The three redirector methods, the ones a person
// switching a companion's backend from the console reaches, were forwarded by nobody's test.
//
// So: every wrapper, in the order a turn stacks them, against every method the port declares.

// fullProvider answers every optional question with a value nothing else in this file produces, so
// a forwarded answer cannot be confused with a wrapper's own default.
type fullProvider struct {
	port.LLMProvider
	cleared []uint64
	base    string
}

func (p *fullProvider) ListModels(context.Context) ([]string, error) {
	return []string{"the-inner-catalogue"}, nil
}
func (p *fullProvider) ProbeContextWindow(context.Context, string) (int, bool) { return 131072, true }
func (p *fullProvider) SetBaseURL(url string) uint64                           { p.base = url; return 77 }
func (p *fullProvider) ClearBaseURL(tok uint64)                                { p.cleared = append(p.cleared, tok) }
func (p *fullProvider) BaseURL() string                                        { return p.base }

// wrappers are the provider wrappers a request actually passes through, alone and stacked.
type providerWrapper struct {
	name string
	wrap func(port.LLMProvider) port.LLMProvider
	// stacked marks the composite. Each layer is idempotent on its own; the stack is not — guarding
	// a metered provider is a guard the meter is not looking for — and it does not need to be,
	// because the order is built at one call site (providerFor) rather than reapplied defensively.
	stacked bool
}

func wrappers() []providerWrapper {
	meter := (&App{}).MeterProvider
	return []providerWrapper{
		{name: "the guard", wrap: GuardProvider},
		{name: "the meter", wrap: meter},
		{name: "the meter over the guard", stacked: true,
			wrap: func(p port.LLMProvider) port.LLMProvider { return meter(GuardProvider(p)) }},
	}
}

// extrasMethods is what this file exercises. Compared against the interface below, so widening
// port.ProviderExtras without extending this test fails instead of quietly leaving the new
// capability unforwarded — which is the exact way the first three were lost.
var extrasMethods = []string{"BaseURL", "ClearBaseURL", "ListModels", "ProbeContextWindow", "SetBaseURL"}

func TestEveryOptionalCapabilityIsExercised(t *testing.T) {
	iface := reflect.TypeOf((*port.ProviderExtras)(nil)).Elem()
	var have []string
	for i := 0; i < iface.NumMethod(); i++ {
		have = append(have, iface.Method(i).Name)
	}
	sort.Strings(have)
	if !reflect.DeepEqual(have, extrasMethods) {
		t.Fatalf("a provider's optional surface is %v but this file forwards-checks %v — the "+
			"difference is a capability every wrapper may be swallowing with nobody looking",
			have, extrasMethods)
	}
}

func TestAWrapperForwardsEveryCapabilityItDoesNotImplement(t *testing.T) {
	ctx := context.Background()
	for _, w := range wrappers() {
		t.Run(w.name, func(t *testing.T) {
			inner := &fullProvider{}
			x, ok := w.wrap(inner).(port.ProviderExtras)
			if !ok {
				t.Fatalf("%s does not offer the optional surface at all", w.name)
			}
			got, err := x.ListModels(ctx)
			if err != nil || len(got) != 1 || got[0] != "the-inner-catalogue" {
				t.Errorf("the catalogue came back as %v (%v)", got, err)
			}
			if n, ok := x.ProbeContextWindow(ctx, "m"); !ok || n != 131072 {
				t.Errorf("the window came back as %d (%v)", n, ok)
			}
			// The redirector is three methods and one story: point it somewhere, read back where
			// requests go now, release the override with the token you were handed.
			tok := x.SetBaseURL("http://elsewhere:1234/v1")
			if tok != 77 {
				t.Errorf("the token came back as %d — 0 means nothing was redirected, which is "+
					"what a caller reads to decide the switch did not happen", tok)
			}
			if u := x.BaseURL(); u != "http://elsewhere:1234/v1" {
				t.Errorf("requests are said to go to %q, so the provider select opens on the "+
					"wrong backend", u)
			}
			x.ClearBaseURL(tok)
			if !reflect.DeepEqual(inner.cleared, []uint64{77}) {
				t.Errorf("the backend was released with %v — an override nobody can release "+
					"outlives the person who installed it", inner.cleared)
			}
		})
	}
}

// And a wrapper over a backend that genuinely cannot do these things says which kind of nothing it
// is, in the shape the port documents: an absence for the catalogue, ok=false for the window, and a
// zero token for a redirect that did not happen. The assertion succeeds either way — the wrapper
// implements the method whether or not what it wraps does — so the VALUE is the whole answer.
func TestAWrapperOverASilentBackendNamesTheAbsence(t *testing.T) {
	ctx := context.Background()
	for _, w := range wrappers() {
		t.Run(w.name, func(t *testing.T) {
			x, ok := w.wrap(noopProvider{}).(port.ProviderExtras)
			if !ok {
				t.Fatalf("%s does not offer the optional surface at all", w.name)
			}
			got, err := x.ListModels(ctx)
			if !errors.Is(err, port.ErrCapabilityAbsent) || got != nil {
				t.Errorf("a backend that cannot list answered (%v, %v) — indistinguishable from "+
					"an empty catalogue", got, err)
			}
			if n, ok := x.ProbeContextWindow(ctx, "m"); ok || n != 0 {
				t.Errorf("a backend that cannot measure answered (%d, %v)", n, ok)
			}
			if tok := x.SetBaseURL("http://elsewhere:1234/v1"); tok != 0 {
				t.Errorf("a backend that cannot be redirected handed out token %d, and a caller "+
					"that releases it releases something else", tok)
			}
			if u := x.BaseURL(); u != "" {
				t.Errorf("a backend that cannot say where it points said %q", u)
			}
			x.ClearBaseURL(0) // releasing a redirect that never happened is a no-op, not a panic
		})
	}
}

// Wrapping is idempotent at both layers, so a call site that wraps defensively neither double-counts
// nor double-guards, and nil stays nil rather than becoming a wrapper around nothing.
func TestWrappingTwiceWrapsOnce(t *testing.T) {
	for _, w := range wrappers() {
		if w.stacked {
			continue
		}
		t.Run(w.name, func(t *testing.T) {
			once := w.wrap(&fullProvider{})
			if twice := w.wrap(once); twice != once {
				t.Errorf("%s wrapped an already-wrapped provider a second time", w.name)
			}
			if got := w.wrap(nil); got != nil {
				t.Errorf("%s of nothing is %#v, not nothing", w.name, got)
			}
		})
	}
}
