package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A provider is more than the one method its port declares. The OpenAI client also answers "which
// models do you have" and "how big is this one's window", and callers reach those by asserting on
// the provider they are holding — which, once the guard wrapped it, was a type with neither. The
// assertion failed and the caller took that for "this backend cannot say": the console's model
// menu came back null and offered nothing to switch to, while the backend behind it listed three
// models to a plain curl.
type listingProvider struct {
	port.LLMProvider
	models []string
	window int
}

func (l listingProvider) ListModels(context.Context) ([]string, error) { return l.models, nil }
func (l listingProvider) ProbeContextWindow(context.Context, string) (int, bool) {
	return l.window, l.window > 0
}

func TestTheGuardForwardsWhatItDoesNotImplement(t *testing.T) {
	inner := listingProvider{models: []string{"opus", "sonnet"}, window: 200000}
	g := GuardProvider(inner)

	lister, ok := g.(interface {
		ListModels(context.Context) ([]string, error)
	})
	if !ok {
		t.Fatal("a guarded provider cannot be asked for its models — the menu has nothing to offer")
	}
	got, err := lister.ListModels(context.Background())
	if err != nil || len(got) != 2 || got[0] != "opus" {
		t.Errorf("the models came back as %v (%v)", got, err)
	}

	prober, ok := g.(interface {
		ProbeContextWindow(context.Context, string) (int, bool)
	})
	if !ok {
		t.Fatal("a guarded provider cannot be asked for a model's window")
	}
	if n, ok := prober.ProbeContextWindow(context.Background(), "opus"); !ok || n != 200000 {
		t.Errorf("the window came back as %d (%v)", n, ok)
	}
}

// A provider that genuinely cannot answer says which kind of nothing it is. Written a few hours
// earlier, this test asserted the older contract — silence indistinguishable from an empty
// catalogue — which is the shape that let an empty menu hide a swallowed question.
func TestAProviderWithNothingToSayNamesTheAbsence(t *testing.T) {
	g := GuardProvider(noopProvider{})
	lister := g.(port.ModelLister)
	got, err := lister.ListModels(context.Background())
	if !errors.Is(err, port.ErrCapabilityAbsent) {
		t.Errorf("a silent provider answered %v (%v)", got, err)
	}
}
