package config

import (
	"os"
	"path/filepath"
	"testing"
)

// [limits] compact_ratio has to survive the round trip from the file, or the knob is decorative —
// which is exactly what it was: app.Config carried the field, nothing outside that package ever
// wrote it, and the TOML had no key at all, so every run compacted at the hardcoded 0.8.
func TestCompactRatioIsReadFromTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
[limits]
max_output_tokens = 4096
context_tokens    = 65536
compact_ratio     = 0.92
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.CompactRatio != 0.92 {
		t.Errorf("compact_ratio = %v, want 0.92", cfg.Limits.CompactRatio)
	}
	// The neighbours must still arrive — a mis-typed struct tag would silently zero one of them.
	if cfg.Limits.MaxOutputTokens != 4096 || cfg.Limits.ContextTokens != 65536 {
		t.Errorf("neighbouring limits lost: %+v", cfg.Limits)
	}
}

// Absent means "use the default", which app.Config.withDefaults fills — config must not invent one.
func TestAnAbsentCompactRatioStaysZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[limits]\ncontext_tokens = 8192\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.CompactRatio != 0 {
		t.Errorf("absent compact_ratio must stay 0 so the app layer defaults it, got %v", cfg.Limits.CompactRatio)
	}
}
