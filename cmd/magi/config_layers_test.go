package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/port"
)

// layerPlat is a Platform that only knows where its directories are, which is all loadConfigLayers
// asks of one.
type layerPlat struct{ cfgDir string }

func (p layerPlat) Exec(context.Context, port.Cmd) (port.ExecResult, error) {
	return port.ExecResult{}, nil
}
func (p layerPlat) ConfigDir() string                        { return p.cfgDir }
func (p layerPlat) DataDir() string                          { return p.cfgDir }
func (p layerPlat) TerminalCaps() port.TermCaps              { return port.TermCaps{} }
func (p layerPlat) ProcessCPUTime(int) (time.Duration, bool) { return 0, false }

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The three files, read in the order the most specific wins.
//
// mergeProjectConfig is tested on its own; what this pins is the LAYERING — that the reader
// actually reaches all three files, in that order, and hands back the companion directory the
// route and permission persisters write into. Those are the parts that live in one function and
// would regress silently: a layer dropped here still starts, still runs, and simply ignores
// somebody's settings.
func TestTheThreeConfigLayersAreReadMostSpecificLast(t *testing.T) {
	cfgDir, wd := t.TempDir(), t.TempDir()
	write(t, cfgDir, "model = \"person-model\"\npermission = \"ask\"\n")
	write(t, filepath.Join(wd, ".magi"), "model = \"team-model\"\n")
	write(t, config.CompanionDir(cfgDir, daemon.WorkspaceKey(wd)), "model = \"my-model\"\n")

	var warn bytes.Buffer
	cfg, companionDir, ok := loadConfigLayers(layerPlat{cfgDir}, wd, &warn)
	if !ok {
		t.Fatalf("three valid files refused to start: %s", warn.String())
	}
	if cfg.Model != "my-model" {
		t.Errorf("model = %q, want my-model — the companion's own file is the most specific layer", cfg.Model)
	}
	if cfg.Permission != "ask" {
		t.Errorf("permission = %q, want ask — a layer that says nothing must not erase the one below",
			cfg.Permission)
	}
	if want := config.CompanionDir(cfgDir, daemon.WorkspaceKey(wd)); companionDir != want {
		t.Errorf("companion dir = %q, want %q — the persisters write there, so a different answer "+
			"writes settings nothing reads back", companionDir, want)
	}
}

// A malformed GLOBAL file stops startup, and says which file.
//
// Falling back to an empty Config would drop the model, [plugins.*] and every other setting the
// person wrote, with no warning — a run that looks fine and is configured by nobody.
func TestAMalformedGlobalConfigRefusesToStart(t *testing.T) {
	cfgDir, wd := t.TempDir(), t.TempDir()
	write(t, cfgDir, "model = \"a\"\nmodel = \"b\"\n") // duplicate key: the whole file fails to parse

	var warn bytes.Buffer
	_, _, ok := loadConfigLayers(layerPlat{cfgDir}, wd, &warn)
	if ok {
		t.Fatal("a global config that cannot be parsed started anyway")
	}
	if got := warn.String(); !strings.Contains(got, filepath.Join(cfgDir, "config.toml")) {
		t.Errorf("the refusal does not name the file to fix: %q", got)
	}
}

// The other two warn and are skipped. A repo-local or per-companion parse error must not take down
// a run whose global config is perfectly valid — the asymmetry is the point, and it is the kind of
// rule that survives a refactor only if something asserts it.
func TestABrokenProjectOrCompanionFileIsSkippedNotFatal(t *testing.T) {
	for _, tc := range []struct{ name, where string }{
		{"project", ".magi"},
		{"companion", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgDir, wd := t.TempDir(), t.TempDir()
			write(t, cfgDir, "model = \"person-model\"\n")
			bad := filepath.Join(wd, tc.where)
			if tc.where == "" {
				bad = config.CompanionDir(cfgDir, daemon.WorkspaceKey(wd))
			}
			write(t, bad, "model = \"a\"\nmodel = \"b\"\n")

			var warn bytes.Buffer
			cfg, _, ok := loadConfigLayers(layerPlat{cfgDir}, wd, &warn)
			if !ok {
				t.Fatalf("a broken %s file stopped a run whose global config was valid", tc.name)
			}
			if cfg.Model != "person-model" {
				t.Errorf("model = %q, want person-model — the valid layer must still stand", cfg.Model)
			}
			if warn.String() == "" {
				t.Errorf("the broken %s file was skipped silently", tc.name)
			}
		})
	}
}
