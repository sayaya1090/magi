package openai

import (
	"encoding/json"
	"testing"
)

// A usage block that mentions a cache, one that does not, and the difference between them.
//
// Measured against the default local backend on 2026-08-07: Ollama's /v1 sends prompt_tokens,
// completion_tokens and total_tokens and no details block at all. So "not reported" is the ordinary
// case here, and it must not be readable as a cache that missed.
func TestUsageSeparatesACacheMissFromABackendThatSaysNothing(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         string
		wantIn       int
		wantCached   int
		wantReported bool
	}{
		{"a hit", `{"prompt_tokens":5000,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":4096}}`,
			5000, 4096, true},
		{"a miss, said out loud", `{"prompt_tokens":5000,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0}}`,
			5000, 0, true},
		{"the local backend's shape — no details block at all",
			`{"prompt_tokens":2417,"completion_tokens":3,"total_tokens":2420}`, 2417, 0, false},
	} {
		var u wireUsage
		if err := json.Unmarshal([]byte(tc.body), &u); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		cached, reported := u.Cached()
		if u.In() != tc.wantIn || cached != tc.wantCached || reported != tc.wantReported {
			t.Errorf("%s: in=%d cached=%d reported=%v, want %d/%d/%v",
				tc.name, u.In(), cached, reported, tc.wantIn, tc.wantCached, tc.wantReported)
		}
	}
}
