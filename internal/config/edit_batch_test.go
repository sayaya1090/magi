package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetKeysCheckedPublishesOrLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	before := []byte("# keep\nmodel = \"old\"\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetKeysChecked(path, "llm.profiles.test", map[string]string{"base_url": "https://example.com", "model": "new", "api_key": "secret"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil || cfg.LLM.Profiles["test"].Model != "new" {
		t.Fatal("batch values did not persist")
	}
	original, _ := os.ReadFile(path)
	if err := SetKeysChecked(path, "llm.profiles.test", map[string]string{"model": "changed", "api_key": "bad\a"}); err == nil {
		t.Fatal("invalid TOML accepted")
	}
	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Fatal("partial update published")
	}
	if !strings.Contains(string(after), "# keep") {
		t.Fatal("comment lost")
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("secret file permissions changed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("staging leaked: %v", entries)
	}
}
