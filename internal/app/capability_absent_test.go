package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// "I cannot say" and "there is nothing" were one answer, and a caller cannot act differently on
// facts it is handed identically. An empty model menu meant both "this backend lists no models"
// and "nobody asked it" — and the second is a bug the first hides, which is how a menu stayed
// empty for as long as the wrapper had existed with nobody able to see why.
func TestAMissingCapabilityIsNotAnEmptyAnswer(t *testing.T) {
	a := &App{llm: noopProvider{}} // implements the port and nothing else
	got, err := a.ListModels(context.Background())
	if !errors.Is(err, port.ErrCapabilityAbsent) {
		t.Fatalf("a backend that cannot list models answered (%v, %v) — indistinguishable from an "+
			"empty catalogue", got, err)
	}
	if got != nil {
		t.Errorf("the absent answer carried a list: %v", got)
	}
}

// And a backend that CAN answer still answers, through both wrappers, in the order a turn uses
// them: the meter wraps the guard wraps the backend.
func TestACapabilitySurvivesBothWrappers(t *testing.T) {
	a := &App{}
	inner := listingProvider{models: []string{"opus"}}
	through := a.MeterProvider(GuardProvider(inner))
	lister, ok := through.(port.ModelLister)
	if !ok {
		t.Fatal("the stack a turn actually runs on cannot be asked for its models")
	}
	got, err := lister.ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0] != "opus" {
		t.Errorf("through guard+meter the catalogue came back as %v (%v)", got, err)
	}
}
