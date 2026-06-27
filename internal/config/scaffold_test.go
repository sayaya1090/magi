package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDefaultIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Writes when absent.
	if err := WriteDefaultIfMissing(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file written: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("written config is empty")
	}
	// The template must parse as valid TOML and yield an (empty) config.
	if _, err := Load(dir); err != nil {
		t.Fatalf("default template does not parse: %v", err)
	}
}

func TestWriteDefaultDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefaultIfMissing(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "model = \"mine\"\n" {
		t.Errorf("existing config was overwritten: %q", string(b))
	}
}

func TestPluginsConfigParses(t *testing.T) {
	dir := t.TempDir()
	toml := `
[plugins.adsso]
endpoint = "https://sso.corp/x"
retries = 3
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	sec := c.Plugins["adsso"]
	if sec == nil {
		t.Fatal("expected [plugins.adsso] section")
	}
	if sec["endpoint"] != "https://sso.corp/x" {
		t.Errorf("endpoint = %v", sec["endpoint"])
	}
}

func TestCouncilEnabledDefaultsOn(t *testing.T) {
	// Unset → on by default.
	if !(CouncilConfig{}).IsEnabled() {
		t.Error("council should be on by default when unset")
	}
	// Explicit false → off.
	off := false
	if (CouncilConfig{Enabled: &off}).IsEnabled() {
		t.Error("enabled=false should disable the council")
	}
	// Explicit true → on.
	on := true
	if !(CouncilConfig{Enabled: &on}).IsEnabled() {
		t.Error("enabled=true should enable the council")
	}
	// Parsed from TOML: enabled = false disables.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[council]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Council.IsEnabled() {
		t.Error("parsed enabled=false should disable the council")
	}
	// No [council] section at all → on.
	if (mustLoadEmpty(t)).Council.IsEnabled() == false {
		t.Error("absent [council] should leave the council on")
	}
}

func mustLoadEmpty(t *testing.T) Config {
	t.Helper()
	c, err := Load(t.TempDir()) // no config.toml
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestThemeConfigParses(t *testing.T) {
	dir := t.TempDir()
	toml := `
[theme.dark]
primary = "#112233"
accent  = "#445566"
[theme.light]
primary = "#aabbcc"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme.Dark["primary"] != "#112233" || c.Theme.Dark["accent"] != "#445566" {
		t.Errorf("theme.dark = %v", c.Theme.Dark)
	}
	if c.Theme.Light["primary"] != "#aabbcc" {
		t.Errorf("theme.light = %v", c.Theme.Light)
	}
}
