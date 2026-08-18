package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/port"
)

// listLLM is a provider that also exposes a model catalog (like the concrete
// *openai.Client), so App.ListModels reaches it through the optional assertion.
type listLLM struct {
	fakeLLM
	models []string
	err    error
}

func (l *listLLM) ListModels(context.Context) ([]string, error) { return l.models, l.err }

func newAppWith(t *testing.T, llm port.LLMProvider) *App {
	t.Helper()
	store, _ := jsonl.New(t.TempDir())
	return New(store, llm, builtin.Default(), bus.New(), nil, Config{})
}

// A provider that implements ListModels has its catalog surfaced verbatim.
func TestListModelsDelegatesWhenSupported(t *testing.T) {
	a := newAppWith(t, &listLLM{models: []string{"gpt-oss:120b-cloud", "qwen3-coder:30b"}})
	got, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "gpt-oss:120b-cloud" || got[1] != "qwen3-coder:30b" {
		t.Fatalf("ListModels = %v, want the provider catalog", got)
	}
}

// The provider's error propagates unchanged (the editor treats it as "no catalog").
func TestListModelsPropagatesError(t *testing.T) {
	want := errors.New("gateway down")
	a := newAppWith(t, &listLLM{err: want})
	if _, err := a.ListModels(context.Background()); !errors.Is(err, want) {
		t.Fatalf("ListModels err = %v, want %v", err, want)
	}
}

// A provider that does NOT implement ListModels says so, rather than answering the way a provider
// with an empty catalogue answers.
//
// This test used to require (nil, nil) — the defect written down as the contract. The two facts
// arrived identically, so no caller could tell them apart, and an empty model menu meant both
// "this backend lists nothing" and "the question never reached it". The second went unnoticed for
// as long as a wrapper had been swallowing it. The suggest box still degrades to profiles and free
// text; what changed is that the reason is now legible to anyone who wants it.
func TestListModelsUnsupportedSaysSoRatherThanLookingEmpty(t *testing.T) {
	a := newAppWith(t, &fakeLLM{}) // fakeLLM has no ListModels method
	got, err := a.ListModels(context.Background())
	if !errors.Is(err, port.ErrCapabilityAbsent) {
		t.Fatalf("ListModels on an unsupported provider = (%v, %v), want ErrCapabilityAbsent", got, err)
	}
	if got != nil {
		t.Errorf("the absent answer carried a list: %v", got)
	}
}
