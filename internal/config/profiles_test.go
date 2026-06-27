package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLMProfilesParse(t *testing.T) {
	dir := t.TempDir()
	toml := `
[llm.profiles.fast]
base_url = "https://fast.gw/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
[llm.profiles.fast.headers]
X-CLIENT-API-KEY = "abc"

[routing]
explore = "fast"
coder   = "qwen3-coder:30b"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := c.LLM.Profiles["fast"]
	if !ok {
		t.Fatal("expected [llm.profiles.fast]")
	}
	if p.BaseURL != "https://fast.gw/v1" || p.Model != "gpt-oss:20b" {
		t.Errorf("profile = %+v", p)
	}
	if p.APIKey != "${FAST_KEY}" {
		t.Errorf("api_key should be raw (expanded later), got %q", p.APIKey)
	}
	if p.Headers["X-CLIENT-API-KEY"] != "abc" {
		t.Errorf("profile headers = %v", p.Headers)
	}
	// Routing references a profile name and a bare model; both are plain strings here.
	if c.Routing["explore"] != "fast" || c.Routing["coder"] != "qwen3-coder:30b" {
		t.Errorf("routing = %v", c.Routing)
	}
}
