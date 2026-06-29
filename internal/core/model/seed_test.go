package model

import "testing"

func TestCloudModelsSeeded(t *testing.T) {
	r := NewRegistry()
	for id, want := range map[string]int{"gpt-oss:120b-cloud": 131072, "gemini-1.5-pro": 2097152, "grok-4": 256000} {
		if got := r.Get(id).ContextWindow; got != want {
			t.Errorf("%s window = %d, want %d", id, got, want)
		}
	}
	// Unknown still falls back to the conservative default.
	if r.Get("totally-unknown-xyz").ContextWindow != 8192 {
		t.Error("unknown model should fall back to 8192")
	}
}
