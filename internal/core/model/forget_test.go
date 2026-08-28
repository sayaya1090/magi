package model

import "testing"

// Forget is exact: a variant a probe registered goes, and the family seed every OTHER variant
// reads through stays. Reaching through to the seed would take a fact off models that never moved.
func TestForgettingAVariantLeavesTheFamilySeed(t *testing.T) {
	r := NewRegistry()
	r.Register(Info{ID: "qwen3-coder", ContextWindow: 262144, Tools: true})
	r.Register(Info{ID: "qwen3-coder:480b-cloud", ContextWindow: 96000, Tools: true})

	if !r.Forget("qwen3-coder:480b-cloud") {
		t.Fatal("Forget reported nothing to forget")
	}
	if r.Has("qwen3-coder:480b-cloud") {
		t.Error("the variant is still registered")
	}
	if got := r.Get("qwen3-coder:480b-cloud").ContextWindow; got != 262144 {
		t.Errorf("the forgotten variant reads %d, want the family seed's 262144 — Forget must "+
			"restore the state before the entry, not leave a hole", got)
	}
	if got := r.Get("qwen3-coder").ContextWindow; got != 262144 {
		t.Errorf("the family seed reads %d; forgetting a variant reached through to it", got)
	}
	if r.Forget("never-registered") {
		t.Error("Forget claimed it removed something that was never there")
	}
}
