package model

import "testing"

func TestCloudModelsSeeded(t *testing.T) {
	r := NewRegistry()
	for id, want := range map[string]int{"gpt-oss:120b-cloud": 131072, "gemini-1.5-pro": 2097152, "grok-4": 256000} {
		if got := r.Get(id).ContextWindow; got != want {
			t.Errorf("%s window = %d, want %d", id, got, want)
		}
	}
	// A truly unknown model reports window 0 = unlimited/unknown (consumers then
	// skip the % gauge and ratio compaction) rather than a tiny guessed number.
	if r.Get("totally-unknown-xyz").ContextWindow != 0 {
		t.Error("unknown model should report window 0 (unlimited)")
	}
}
