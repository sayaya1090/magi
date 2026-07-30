package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sayaya1090/magi/internal/core/model"
)

// [limits] context_tokens is the operator's number for the run, and every consumer of the window
// — the compaction trigger, the live meter, /context, the model list — reads it through
// contextWindow. Resolving it there is what makes "every model" true: baking it into one registry
// entry at startup covered the configured model and left anything reached later by /route on its
// seeded number.
func TestContextTokensOverridesTheWindowForEveryModel(t *testing.T) {
	var probes atomic.Int32
	a := &App{
		probingWindows: map[string]struct{}{},
		cfg: Config{
			Models:        model.NewRegistry(),
			ContextTokens: 40000,
			ContextWindowProber: func(_ context.Context, _ string) (int, bool) {
				probes.Add(1)
				return 200000, true
			},
		},
	}

	// A seeded model: 262144 in the registry, 40000 by configuration.
	if w := a.contextWindow("qwen3-coder:30b"); w != 40000 {
		t.Errorf("seeded model window = %d, want the configured 40000", w)
	}
	// magi's own default, the one the old code could never reach.
	if w := a.contextWindow("gpt-oss:120b-cloud"); w != 40000 {
		t.Errorf("default model window = %d, want the configured 40000", w)
	}
	// A model magi has never heard of: answered from configuration, with no probe at all —
	// there is nothing to ask a backend about once the operator has said what the window is.
	if w := a.contextWindow("mystery:latest"); w != 40000 {
		t.Errorf("unseeded model window = %d, want the configured 40000", w)
	}
	if got := probes.Load(); got != 0 {
		t.Errorf("no probe is needed when the window is configured, got %d", got)
	}
	// An empty id is still "no model", not the override.
	if w := a.contextWindow(""); w != 0 {
		t.Errorf("empty id = %d, want 0", w)
	}
}

// Unset means unset: the registry and the probe keep their old authority.
func TestWithoutContextTokensTheRegistryStillDecides(t *testing.T) {
	a := &App{
		probingWindows: map[string]struct{}{},
		cfg:            Config{Models: model.NewRegistry()},
	}
	if w := a.contextWindow("qwen3-coder:30b"); w != 262144 {
		t.Errorf("seeded window = %d, want 262144", w)
	}
	if w := a.contextWindow("gpt-oss:120b-cloud"); w != 131072 {
		t.Errorf("default window = %d, want 131072", w)
	}
}
