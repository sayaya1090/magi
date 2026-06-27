package model

import "testing"

func TestGetKnownAndUnknown(t *testing.T) {
	r := NewRegistry()

	known := r.Get("qwen3-coder:30b")
	if known.ContextWindow != 262144 || !known.Tools {
		t.Errorf("known model meta wrong: %+v", known)
	}

	unknown := r.Get("some-random-model")
	if unknown.ContextWindow == 0 || !unknown.Tools {
		t.Errorf("unknown model should get usable defaults: %+v", unknown)
	}
	if unknown.ID != "some-random-model" {
		t.Errorf("unknown.ID=%q", unknown.ID)
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
