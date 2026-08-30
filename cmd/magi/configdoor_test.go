package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
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
	write(t, filepath.Join(d.workdir, ".magi"), "embed_model = \"project-one\"\n")
	if v, src, _ := itemOf(t, d, "embed_model"); v != "project-one" || src != "project" {
		t.Fatalf("the workspace file must beat the account's, got %q from %q", v, src)
	}
	// The companion layer is keyed by the workspace, and the key is not a name somebody picks —
	// pointing this at "ws" wrote an inert file and the assertion below passed for the wrong
	// reason, which is how the tier order came to have no test at all.
	write(t, config.CompanionDir(cfgDir, daemon.WorkspaceKey(d.workdir)), "embed_model = \"companion-one\"\n")
	if v, src, _ := itemOf(t, d, "embed_model"); v != "companion-one" || src != "companion" {
		t.Fatalf("the companion layer is merged last and must win, got %q from %q", v, src)
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

// A layer that will not parse is said out loud. A file with a typo in it and a file that says
// nothing are the same absence to a screen shown only values — and this is the door whose promise
// is that the screen sees what the daemon read.
func TestAnUnparseableLayerIsReported(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "embed_model = \"nomic\nthis is not toml\n")
	items, err := d.ConfigHere(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Unreadable == "" {
			t.Fatalf("%s was reported as simply unset while its file will not parse", it.Key)
		}
		if !strings.Contains(it.Unreadable, "global") {
			t.Errorf("the reader must name the layer: %q", it.Unreadable)
		}
	}
}

// A write that would leave a config.toml the daemon cannot read is refused, and the file is left
// as it was: a broken GLOBAL file stops magi starting for every workspace on the machine.
func TestAWriteThatWouldBreakTheFileIsPutBack(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "embed_model = \"before\"\n")
	// A control byte is what a pasted template carries; the gate drops it, so the value lands
	// clean rather than bricking the file.
	item, err := d.ConfigSet(context.Background(), "embed_model", "nomic\x07embed", "global")
	if err != nil {
		t.Fatal(err)
	}
	if item.Value != "nomicembed" || item.Unreadable != "" {
		t.Fatalf("the control byte survived into the file: %+v", item)
	}
	if _, lerr := config.Load(cfgDir); lerr != nil {
		t.Fatalf("the file no longer parses: %v", lerr)
	}
}

// The read-back is a read, not an echo: what comes back is what the daemon can see now.
func TestTheAnswerIsReadBackNotEchoed(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "embed_model = \"old\"\n")
	t.Setenv("MAGI_EMBED_MODEL", "env-wins")
	item, err := d.ConfigSet(context.Background(), "embed_model", "new", "global")
	if err != nil {
		t.Fatal(err)
	}
	if item.Value != "env-wins" || item.Source != "env" {
		t.Fatalf("the write echoed itself instead of reading back: %+v", item)
	}
	if !strings.Contains(item.Doc, "MAGI_EMBED_MODEL") {
		t.Errorf("a write the environment overrules must say so: %q", item.Doc)
	}
	if item.Applies != "next start" {
		t.Errorf("applies is the key's own property, got %q", item.Applies)
	}
	if item.File == "" || !strings.HasPrefix(item.File, cfgDir) {
		t.Errorf("the answer must name the file a write lands in, got %q", item.File)
	}
}

// The untrusted workspace's own values are not read either — the engine drops them before the
// merge, so reporting one as effective would be the screen lying about what runs.
func TestAnUntrustedWorkspaceValueIsNotReported(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	write(t, cfgDir, "[autocomplete]\ncode_profile = \"global-fast\"\n")
	write(t, filepath.Join(d.workdir, ".magi"), "[autocomplete]\ncode_profile = \"project-fast\"\n")
	if v, src, _ := itemOf(t, d, "autocomplete.code_profile"); v != "global-fast" || src != "global" {
		t.Fatalf("an untrusted workspace value was reported as effective: %q from %q", v, src)
	}
	// Its profiles are dropped for the same reason.
	write(t, filepath.Join(d.workdir, ".magi"), "[llm.profiles.sneaky]\nmodel = \"x\"\n")
	list, err := d.ProfilesHere(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Name == "sneaky" {
			t.Fatal("an untrusted workspace's profile was offered as assignable")
		}
	}
}

// The project tier's directory is a directory in somebody's repository, not a private one.
func TestTheWorkspaceConfigDirIsGroupReadable(t *testing.T) {
	d, _, wd := settingsEngine(t)
	if err := os.RemoveAll(filepath.Join(wd, ".magi")); err != nil {
		t.Fatal(err)
	}
	trustWorkspace(t, d)
	if _, err := d.ConfigSet(context.Background(), "autocomplete.code_profile", "fast", "project"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(wd, ".magi"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf(".magi was created %v; every other creator of it makes it 0755", fi.Mode().Perm())
	}
}

// trustWorkspace records this workspace as the operator's own, the way `magi --trust` does.
func trustWorkspace(t *testing.T, d daemonEngine) {
	t.Helper()
	if _, err := config.Trust(d.configDir, d.workdir); err != nil {
		t.Fatal(err)
	}
}
