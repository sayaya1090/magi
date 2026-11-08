package openai

import "testing"

func TestPromptCacheTTLSetting(t *testing.T) {
	for env, want := range map[string]string{"": "1h", "5m": "5m", "default": ""} {
		t.Setenv("MAGI_PROMPT_CACHE_TTL", env)
		if got := ephemeral().TTL; got != want {
			t.Errorf("env %q: got %q want %q", env, got, want)
		}
	}
}
