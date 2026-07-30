package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Both [limits] keys have to survive the round trip from the file the operator edits.
func TestLimitsSectionParses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("[limits]\nmax_output_tokens = 4096\ncontext_tokens = 40000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits.MaxOutputTokens != 4096 {
		t.Errorf("max_output_tokens = %d, want 4096", c.Limits.MaxOutputTokens)
	}
	if c.Limits.ContextTokens != 40000 {
		t.Errorf("context_tokens = %d, want 40000", c.Limits.ContextTokens)
	}

	// An absent section leaves both at zero, which every consumer reads as "not configured".
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "config.toml"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Limits.MaxOutputTokens != 0 || c2.Limits.ContextTokens != 0 {
		t.Errorf("absent [limits] is zero, got %+v", c2.Limits)
	}
}
