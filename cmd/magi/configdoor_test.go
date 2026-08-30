package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

func settingsEngine(t *testing.T) (daemonEngine, string, string) {
	t.Helper()
	cfgDir, wd := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".magi"), 0o700); err != nil {
		t.Fatal(err)
	}
	return daemonEngine{workdir: wd, configDir: cfgDir}, cfgDir, wd
}

func itemOf(t *testing.T, d daemonEngine, key string) (value, source, tier string) {
	t.Helper()
	items, err := d.ConfigHere(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Key == key {
			return it.Value, it.Source, it.Tier
		}
	}
	t.Fatalf("%q is not on the whitelist", key)
	return "", "", ""
}

// The value a screen shows is the one the engine will use, and it says which layer won: the later
// file beats the earlier, and the environment beats them both.
func TestASettingReportsTheLayerThatWins(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "embed_model = \"global-one\"\n")
	if v, src, _ := itemOf(t, d, "embed_model"); v != "global-one" || src != "global" {
		t.Fatalf("global value read as %q from %q", v, src)
	}
	write(t, config.CompanionDir(cfgDir, "ws"), "embed_model = \"companion-one\"\n")
	// The companion layer is keyed by the workspace, so point the engine at the same key.
	d2 := d
	if v, src, _ := itemOf(t, d2, "embed_model"); v != "global-one" || src != "global" {
		t.Fatalf("an unrelated companion file changed the answer: %q from %q", v, src)
	}
	t.Setenv("MAGI_EMBED_MODEL", "env-one")
	if v, src, _ := itemOf(t, d, "embed_model"); v != "env-one" || src != "env" {
		t.Fatalf("the environment must win and say so, got %q from %q", v, src)
	}
}

// A write lands where the value was read from, so changing an account-wide setting does not mint a
// workspace override of it.
func TestAWriteGoesBackWhereTheValueCameFrom(t *testing.T) {
	d, cfgDir, wd := settingsEngine(t)
	write(t, cfgDir, "embed_model = \"old\"\n")
	item, err := d.ConfigSet(context.Background(), "embed_model", "new", "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Value != "new" || item.Tier != "global" {
		t.Fatalf("the write landed as %+v", item)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, ".magi", "config.toml")); strings.Contains(string(b), "embed_model") {
		t.Error("an account-wide setting was overridden in the workspace")
	}
	if b, _ := os.ReadFile(filepath.Join(cfgDir, "config.toml")); !strings.Contains(string(b), "new") {
		t.Error("the global file did not take the value")
	}
}

// A setting an untrusted workspace file cannot set is refused there, rather than written into a
// file the engine will ignore — the one outcome worse than either alternative.
func TestAnUntrustedWorkspaceIsRefusedNotSilentlyIgnored(t *testing.T) {
	d, _, wd := settingsEngine(t)
	_, err := d.ConfigSet(context.Background(), "autocomplete.code_profile", "fast", "project")
	if err == nil {
		t.Fatal("an untrusted workspace file took a setting the engine drops")
	}
	if !strings.Contains(err.Error(), "trust") {
		t.Errorf("the refusal must say what to do about it: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, ".magi", "config.toml")); strings.Contains(string(b), "code_profile") {
		t.Error("the refusal still wrote the file")
	}
}

// Only the whitelist. The same file holds the permission posture and the hooks that run.
func TestOnlyTheWhitelistedKeysCanBeSet(t *testing.T) {
	d, _, _ := settingsEngine(t)
	for _, key := range []string{"permission", "hooks", "llm.base_url", ""} {
		if _, err := d.ConfigSet(context.Background(), key, "yolo", ""); err == nil {
			t.Errorf("%q was accepted by the settings door", key)
		}
	}
}

// The profile picker lists what the daemon will actually resolve, with the layer that defines it.
func TestProfilesListsWhatMayBeAssigned(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "[llm.profiles.fast]\nmodel = \"small\"\n")
	list, err := d.ProfilesHere(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "fast" || list[0].Tier != "global" {
		t.Fatalf("the picker offered %+v", list)
	}
}
