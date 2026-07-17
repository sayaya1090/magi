package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/config"
)

// loadDoctorProbes must load plugins with REAL (discard) registries and report
// each source. The regression it guards: the throwaway -doctor host passed nil
// ContextReg/ModelReg/UserReg, so a plugin touching them at load time failed to
// load and its probes silently vanished from the report; embedded plugins were
// never loaded at all. A user-directory plugin and the embedded set must both
// show up, and a broken plugin must surface as a fail row, not disappear.
func TestLoadDoctorProbesReportsAllSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	wd := t.TempDir()

	// One loadable plugin (registers a context provider AT LOAD TIME — the exact
	// shape that used to fail on the nil registry) and one broken plugin.
	pluginsDir := t.TempDir()
	good := filepath.Join(pluginsDir, "goodplug")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(good, "plugin.toml"),
		[]byte("name = \"goodplug\"\nversion = \"0.0.1\"\ncapabilities = [\"context-provider\", \"doctor\"]\n"), 0o644)
	os.WriteFile(filepath.Join(good, "init.lua"), []byte(
		`magi.register_context_provider{name = "gp", provide = function(q) return "" end}
magi.register_doctor_probes{ gpcheck = function() return "ok", "all good" end }`), 0o644)
	bad := filepath.Join(pluginsDir, "badplug")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bad, "plugin.toml"),
		[]byte("name = \"badplug\"\nversion = \"0.0.1\"\n"), 0o644)
	os.WriteFile(filepath.Join(bad, "init.lua"), []byte(`this is not lua ((`), 0o644)

	probes, report := loadDoctorProbes(config.Config{}, platform.New(), wd, pluginsDir, nil)

	// The original bug: a plugin that loads must get its doctor probes into the
	// collected set — nil registries made the load fail and the probes vanish.
	foundProbe := false
	for _, p := range probes {
		if strings.Contains(p.Name(), "gpcheck") {
			foundProbe = true
		}
	}
	if !foundProbe {
		names := make([]string, 0, len(probes))
		for _, p := range probes {
			names = append(names, p.Name())
		}
		t.Errorf("goodplug's doctor probe must be collected, got %v", names)
	}

	var loadedGood, failedBad bool
	for _, c := range report {
		if c.Name != "plugins" {
			t.Errorf("load-report rows must be named 'plugins', got %q", c.Name)
		}
		switch {
		case c.Status == "ok" && strings.Contains(c.Detail, "goodplug"):
			loadedGood = true
		case c.Status == "fail" && strings.Contains(c.Detail, "badplug"):
			failedBad = true
		}
		// Embedded plugins load through the same helper; a load failure there
		// would surface as a fail row naming the embedded source.
		if c.Status == "fail" && strings.Contains(c.Detail, "embedded") {
			t.Errorf("embedded plugin load must not fail: %+v", c)
		}
	}
	if !loadedGood {
		t.Errorf("a plugin registering a context provider at load time must load and be reported; report: %+v", report)
	}
	if !failedBad {
		t.Errorf("a broken plugin must surface as a fail row, not vanish; report: %+v", report)
	}
}
