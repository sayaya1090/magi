package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// reload_config re-reads the on-disk model and applies it to the session (via the
// registry), syncs magi.model(), and queues a reload_config UI effect.
func TestReloadConfigAppliesModel(t *testing.T) {
	cfg := writeConfig(t, `model = "qwen3-coder:30b"`+"\n")
	reg := &fakeModelReg{}
	h, out := loadHost(t, HostConfig{ConfigPath: cfg, ModelReg: reg, Runtime: RuntimeInfo{Model: "old"}},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok = magi.reload_config()
magi.log("ok=" .. tostring(ok) .. " model=" .. magi.model())`)
	if reg.got != "qwen3-coder:30b" {
		t.Errorf("registry got %q, want the reloaded model", reg.got)
	}
	if !strings.Contains(out, "ok=true model=qwen3-coder:30b") {
		t.Errorf("reload should apply and sync magi.model(): %q", out)
	}
	if e := h.TakeUIEffects(); len(e) != 1 || e[0] != "reload_config" {
		t.Errorf("effects = %v, want [reload_config]", e)
	}
}

// Without the config:write:model grant, reload_config is refused and nothing is
// applied.
func TestReloadConfigNeedsGrant(t *testing.T) {
	cfg := writeConfig(t, `model = "x"`+"\n")
	reg := &fakeModelReg{}
	h, out := loadHost(t, HostConfig{ConfigPath: cfg, ModelReg: reg},
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local ok, e = magi.reload_config()
magi.log("e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("ungranted reload must not apply a model, got %q", reg.got)
	}
	if !strings.Contains(out, "permission denied: config:write:model") {
		t.Errorf("expected permission-denied: %q", out)
	}
	if e := h.TakeUIEffects(); len(e) != 0 {
		t.Errorf("refused reload must not queue an effect, got %v", e)
	}
}

// A corrupt config surfaces as (nil, err); the session keeps its model and no effect
// is queued (so a bad edit can't blank the running model).
func TestReloadConfigParseErrorBacksOff(t *testing.T) {
	// Duplicate top-level key → whole-file TOML parse fails.
	cfg := writeConfig(t, `model = "a"`+"\n"+`model = "b"`+"\n")
	reg := &fakeModelReg{}
	h, out := loadHost(t, HostConfig{ConfigPath: cfg, ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.reload_config()
magi.log("e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("parse error must not apply a model, got %q", reg.got)
	}
	if !strings.Contains(out, "cannot parse config") {
		t.Errorf("expected a parse error: %q", out)
	}
	if e := h.TakeUIEffects(); len(e) != 0 {
		t.Errorf("failed reload must not queue an effect, got %v", e)
	}
}
