package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadManifestValid(t *testing.T) {
	dir := writeManifest(t, `
name = "demo"
version = "1.0"
capabilities = ["tool"]
permissions = ["exec:git"]
entry = "main.lua"
`)
	m, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "demo" || m.Entry != "main.lua" || len(m.Capabilities) != 1 || m.Permissions[0] != "exec:git" {
		t.Fatalf("parsed wrong: %+v", m)
	}
}

func TestLoadManifestEntryDefaults(t *testing.T) {
	dir := writeManifest(t, `name = "demo"`) // no entry
	m, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Entry != "init.lua" {
		t.Fatalf("entry should default to init.lua, got %q", m.Entry)
	}
}

func TestLoadManifestMissingName(t *testing.T) {
	dir := writeManifest(t, `version = "1.0"`) // no name
	_, err := loadManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "missing name") {
		t.Fatalf("want missing-name error, got %v", err)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	_, err := loadManifest(t.TempDir()) // no plugin.toml at all
	if err == nil || !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("want read error, got %v", err)
	}
}

func TestLoadManifestInvalidTOML(t *testing.T) {
	dir := writeManifest(t, `name = "demo" version = oops`) // malformed
	_, err := loadManifest(dir)
	if err == nil || !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("want parse error, got %v", err)
	}
}
