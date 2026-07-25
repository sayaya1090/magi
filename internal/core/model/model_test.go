package model

import "testing"

func TestGetKnownAndUnknown(t *testing.T) {
	r := NewRegistry()

	known := r.Get("qwen3-coder:30b")
	if known.ContextWindow != 262144 || !known.Tools {
		t.Errorf("known model meta wrong: %+v", known)
	}

	// A truly unknown model (no exact seed, no family) reports window 0 =
	// unlimited/unknown, so consumers skip the % gauge and ratio compaction rather
	// than measuring against a tiny guessed 8K (which read as "context 500%").
	unknown := r.Get("some-random-model")
	if unknown.ContextWindow != 0 {
		t.Errorf("unknown model window=%d, want 0 (unlimited)", unknown.ContextWindow)
	}
	if !unknown.Tools {
		t.Errorf("unknown model should still default Tools=true: %+v", unknown)
	}
	if unknown.ID != "some-random-model" {
		t.Errorf("unknown.ID=%q", unknown.ID)
	}
}

// A cloud/size variant that isn't seeded inherits its family's window instead of
// falling to the generic default — the fix for the "context 500%" report where
// e.g. an unseeded qwen3-coder tag got an 8K window against a huge real context.
func TestGetFamilyFallback(t *testing.T) {
	r := NewRegistry()
	v := r.Get("qwen3-coder:480b-cloud") // not seeded; family "qwen3-coder" is
	if v.ContextWindow != 262144 {
		t.Errorf("variant should inherit qwen3-coder window 262144, got %d", v.ContextWindow)
	}
	if v.ID != "qwen3-coder:480b-cloud" {
		t.Errorf("family match should keep the requested id, got %q", v.ID)
	}
	// An exact seed still wins over the family heuristic.
	if got := r.Get("gpt-oss:120b-cloud").ContextWindow; got != 131072 {
		t.Errorf("exact seed window=%d want 131072", got)
	}
}

func TestCost(t *testing.T) {
	// Local model: free.
	if c := NewRegistry().Get("qwen3-coder:30b").Cost(1000, 1000); c != 0 {
		t.Errorf("local cost=%v want 0", c)
	}
	// Priced model: 1M in @ $5 + 1M out @ $25 = $30.
	c := NewRegistry().Get("claude-opus-4-8").Cost(1_000_000, 1_000_000)
	if c != 30 {
		t.Errorf("opus cost=%v want 30", c)
	}
}

func TestRegister(t *testing.T) {
	r := NewRegistry()
	r.Register(Info{ID: "custom", ContextWindow: 4096, Tools: true})
	if !r.Has("custom") {
		t.Fatal("custom not registered")
	}
	if r.Get("custom").ContextWindow != 4096 {
		t.Errorf("custom window wrong")
	}
}

// RegisterIfAbsent registers only when the exact ID is absent, so a background probe can never clobber
// a value a concurrent manual override already set.
func TestRegisterIfAbsent(t *testing.T) {
	r := NewRegistry()
	if !r.RegisterIfAbsent(Info{ID: "m", ContextWindow: 1000}) {
		t.Fatal("first RegisterIfAbsent on an absent id must register")
	}
	if r.RegisterIfAbsent(Info{ID: "m", ContextWindow: 9999}) {
		t.Error("RegisterIfAbsent on a present id must NOT overwrite")
	}
	if got := r.Get("m").ContextWindow; got != 1000 {
		t.Errorf("present value must be preserved, got %d", got)
	}
	// Unconditional Register (the manual override) still wins over a prior value.
	r.Register(Info{ID: "m", ContextWindow: 5000})
	if got := r.Get("m").ContextWindow; got != 5000 {
		t.Errorf("Register must overwrite (manual override wins), got %d", got)
	}
}
