package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
)

// A companion's own settings are keyed the same way its door is.
//
// Two things read this key — the socket and the settings directory — and a companion whose key was
// spelled two ways would answer on one door and read what was written for another.
func TestACompanionsSettingsAreKeyedLikeItsSocket(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	key := daemon.WorkspaceKey(wd)
	if key == "" {
		t.Fatal("a workspace with no key")
	}
	sock := daemon.SocketPath(cfg, wd)
	if want := filepath.Join(cfg, "daemon-"+key+".sock"); sock != want {
		t.Errorf("socket = %q, want %q", sock, want)
	}
	if want := filepath.Join(cfg, "companions", key); config.CompanionDir(cfg, key) != want {
		t.Errorf("settings = %q, want %q", config.CompanionDir(cfg, key), want)
	}
	// And it is OUTSIDE the workspace, which is the whole point: the file tools are jailed to the
	// workspace, so a file inside it is one the agent can rewrite.
	if rel, err := filepath.Rel(wd, config.CompanionDir(cfg, key)); err == nil &&
		rel != ".." && !filepath.IsAbs(rel) && rel[0] != '.' {
		t.Errorf("a companion's settings live inside its own workspace (%q)", rel)
	}
}

// The three layers, in the order the most specific wins.
//
// The person's file is the baseline, the team's project file refines it, and this operator's
// choice about THIS companion is last — the same shape as the socket: one workspace, one door, one
// set of settings for it.
func TestTheCompanionLayerWinsOverTheProjectAndTheGlobal(t *testing.T) {
	person := config.Config{Model: "person-model", Permission: "ask"}
	team := config.Config{Model: "team-model"}
	mine := config.Config{Model: "my-model"}

	got := mergeProjectConfig(mergeProjectConfig(person, team), mine)
	if got.Model != "my-model" {
		t.Errorf("model = %q; the companion's own choice must be the one that runs", got.Model)
	}
	// And with nothing of its own, the team's stands.
	got = mergeProjectConfig(mergeProjectConfig(person, team), config.Config{})
	if got.Model != "team-model" {
		t.Errorf("model = %q; an empty companion file must change nothing", got.Model)
	}
}

// "always, in this project" no longer writes into the project.
//
// It appended to <workspace>/.magi/config.toml: an approval dirtied the user's git tree, and a
// decision one person made on one machine was offered to the whole team in a diff.
func TestPersistWritesTheCompanionsFileAndNotTheRepo(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	own := filepath.Join(config.CompanionDir(cfg, daemon.WorkspaceKey(wd)), "config.toml")
	if err := (permPersister{path: own}).PersistAllow("bash(go test:*)"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, ".magi", "config.toml")); !os.IsNotExist(err) {
		t.Error("approving a tool wrote into the user's workspace")
	}
	body, err := os.ReadFile(own)
	if err != nil {
		t.Fatalf("the rule went nowhere: %v", err)
	}
	if got := string(body); !strings.Contains(got, "bash(go test:*)") {
		t.Errorf("the companion's file does not hold the rule:\n%s", got)
	}
	// And it is read back by the ordinary loader, which is what makes it survive a restart.
	back, err := config.Load(config.CompanionDir(cfg, daemon.WorkspaceKey(wd)))
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Allow) != 1 || back.Allow[0] != "bash(go test:*)" {
		t.Errorf("the rule does not load back: %v", back.Allow)
	}
}
