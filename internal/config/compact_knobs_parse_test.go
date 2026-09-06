package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The two knobs beside compact_ratio: how much of the budget a fold keeps verbatim, and which
// model writes the brief. Read from the file like the ratio; absent stays zero so the app layer
// defaults them.
func TestCompactKeepAndModelAreReadFromTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[limits]
compact_keep  = 0.4
compact_model = "small-fast"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.CompactKeep != 0.4 || cfg.Limits.CompactModel != "small-fast" {
		t.Errorf("limits = %+v, want keep 0.4 and model small-fast", cfg.Limits)
	}
}
