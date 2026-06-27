package lua

import (
	"context"
	"strings"
	"testing"
)

func hostWithConfig(t *testing.T, configs map[string]map[string]any) (*Host, string) {
	t.Helper()
	dataDir := t.TempDir()
	return NewHostWithConfig(HostConfig{
		PluginConfigs: configs,
		DataDir:       dataDir,
		Runtime:       RuntimeInfo{Workdir: t.TempDir()},
		Logf:          func(string) {},
	}), dataDir
}

// config_get reads the [plugins.<name>] section from config.toml.
func TestPluginConfigGetFromConfig(t *testing.T) {
	h, _ := hostWithConfig(t, map[string]map[string]any{
		"p": {"endpoint": "https://corp/x", "retries": int64(3)},
	})
	var logged strings.Builder
	h.logf = func(s string) { logged.WriteString(s + "\n") }
	dir := writePlugin(t, `name="p"`+"\n"+`capabilities=["tool"]`,
		`magi.log("ep=" .. magi.config_get("endpoint") .. " r=" .. tostring(magi.config_get("retries")))`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(logged.String(), "ep=https://corp/x") || !strings.Contains(logged.String(), "r=3") {
		t.Errorf("config_get did not read config.toml section: %q", logged.String())
	}
}

// config_get returns the provided default when the key is absent.
func TestPluginConfigGetDefault(t *testing.T) {
	h, _ := hostWithConfig(t, nil)
	var logged strings.Builder
	h.logf = func(s string) { logged.WriteString(s + "\n") }
	dir := writePlugin(t, `name="p"`+"\n"+`capabilities=["tool"]`,
		`magi.log("v=" .. magi.config_get("missing", "fallback"))`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(logged.String(), "v=fallback") {
		t.Errorf("config_get should return default: %q", logged.String())
	}
}

// config_set persists a value; config_get reads it back (across a fresh load,
// mimicking a downloaded per-user config saved to disk), and the persisted
// store overrides the config.toml section.
func TestPluginConfigSetPersistsAndOverrides(t *testing.T) {
	configs := map[string]map[string]any{"p": {"token": "from-config"}}
	h, dataDir := hostWithConfig(t, configs)

	// First load: save a downloaded token.
	dir := writePlugin(t, `name="p"`+"\n"+`capabilities=["tool"]`,
		`magi.config_set("token", "downloaded-123")`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load1: %v", err)
	}

	// Second host (fresh process) with the SAME data dir reads the persisted value,
	// which must override the config.toml section.
	h2 := NewHostWithConfig(HostConfig{PluginConfigs: configs, DataDir: dataDir, Logf: func(string) {}})
	var logged strings.Builder
	h2.logf = func(s string) { logged.WriteString(s + "\n") }
	dir2 := writePlugin(t, `name="p"`+"\n"+`capabilities=["tool"]`,
		`magi.log("token=" .. magi.config_get("token"))`,
	)
	if _, err := h2.Load(context.Background(), dir2); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if !strings.Contains(logged.String(), "token=downloaded-123") {
		t.Errorf("persisted store should override config and survive reload: %q", logged.String())
	}
}
