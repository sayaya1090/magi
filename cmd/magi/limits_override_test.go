package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	coremodel "github.com/sayaya1090/magi/internal/core/model"
)

// [limits] is the operator's answer about this run, and it has to apply to the model actually
// running. The override used to be read only inside the "magi has never heard of this model"
// branch, so `context_tokens` did nothing at all for any of the sixteen seeded ids — including
// magi's own default, gpt-oss:120b-cloud. Nothing was said about it either: the value was parsed,
// carried, and dropped.
func TestLimitsOverrideAppliesToASeededModelToo(t *testing.T) {
	probed := func(n int, ok bool) (func() (int, bool), *int) {
		calls := 0
		return func() (int, bool) { calls++; return n, ok }, &calls
	}

	t.Run("seeded model, explicit window", func(t *testing.T) {
		probe, calls := probed(0, false)
		w, mo, ok := resolveWindowOverride(true, probe, config.LimitsConfig{ContextTokens: 40000})
		if !ok || w != 40000 {
			t.Fatalf("the configured window is the one that applies: w=%d ok=%v", w, ok)
		}
		if mo != 10000 {
			t.Errorf("output budget defaults to a quarter of the window, got %d", mo)
		}
		if *calls != 0 {
			t.Errorf("a model magi already has metadata for is not probed: %d calls", *calls)
		}
	})

	t.Run("seeded model, nothing configured", func(t *testing.T) {
		probe, calls := probed(999, true)
		if _, _, ok := resolveWindowOverride(true, probe, config.LimitsConfig{}); ok {
			t.Error("with no override there is nothing to register — the seeded window stands")
		}
		if *calls != 0 {
			t.Errorf("and still no probe: %d calls", *calls)
		}
	})

	t.Run("unseeded model, probe answers", func(t *testing.T) {
		probe, calls := probed(131072, true)
		w, mo, ok := resolveWindowOverride(false, probe, config.LimitsConfig{})
		if !ok || w != 131072 || mo != 32768 {
			t.Fatalf("the probe's window is used: w=%d mo=%d ok=%v", w, mo, ok)
		}
		if *calls != 1 {
			t.Errorf("probed exactly once, got %d", *calls)
		}
	})

	t.Run("unseeded model, probe fails, no override", func(t *testing.T) {
		probe, _ := probed(0, false)
		if _, _, ok := resolveWindowOverride(false, probe, config.LimitsConfig{}); ok {
			t.Error("nothing measured and nothing configured means nothing to say")
		}
	})

	t.Run("unseeded model, probe fails, override rescues", func(t *testing.T) {
		probe, _ := probed(0, false)
		w, _, ok := resolveWindowOverride(false, probe, config.LimitsConfig{ContextTokens: 8192})
		if !ok || w != 8192 {
			t.Fatalf("the configured window stands in for a probe that could not answer: w=%d ok=%v", w, ok)
		}
	})

	t.Run("both limits set", func(t *testing.T) {
		probe, _ := probed(0, false)
		w, mo, ok := resolveWindowOverride(true, probe, config.LimitsConfig{ContextTokens: 60000, MaxOutputTokens: 4096})
		if !ok || w != 60000 || mo != 4096 {
			t.Fatalf("an explicit output cap replaces the quarter-window default: w=%d mo=%d ok=%v", w, mo, ok)
		}
	})
}

// The registry write that follows keeps what the seed knew about the model — an override is about
// the window, not about whether the thing supports tools or what it costs.
func TestOverridingASeededWindowKeepsTheRestOfItsMetadata(t *testing.T) {
	reg := coremodel.NewRegistry()
	const id = "gpt-4o" // seeded with Vision and pricing
	before := reg.Get(id)
	if !before.Vision || before.InputCost == 0 {
		t.Fatalf("fixture assumes a seeded model with metadata worth keeping: %+v", before)
	}

	w, mo, ok := resolveWindowOverride(reg.Has(id), func() (int, bool) { return 0, false },
		config.LimitsConfig{ContextTokens: 12345})
	if !ok {
		t.Fatal("the override applies")
	}
	info := reg.Get(id)
	info.ContextWindow, info.MaxOutput = w, mo
	reg.Register(info)

	got := reg.Get(id)
	if got.ContextWindow != 12345 {
		t.Errorf("window is the configured one, got %d", got.ContextWindow)
	}
	if !got.Vision || got.InputCost != before.InputCost || !got.Tools {
		t.Errorf("the rest of the seed survives: %+v", got)
	}
}
