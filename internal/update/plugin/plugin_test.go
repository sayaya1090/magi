package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func haveGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// run executes git in dir and fails the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Deterministic identity/branch so commits work in a bare CI env.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeManifest(t *testing.T, dir, name, version string) {
	t.Helper()
	body := "name = \"" + name + "\"\nversion = \"" + version + "\"\nentry = \"init.lua\"\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte("-- x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeRemote builds an "upstream" git repo (a normal working repo other clones can
// fetch from over the filesystem) with a plugin.toml, and returns its path.
func makeRemote(t *testing.T, name, version string) string {
	t.Helper()
	remote := t.TempDir()
	run(t, remote, "init", "-q", "-b", "main")
	writeManifest(t, remote, name, version)
	run(t, remote, "add", "-A")
	run(t, remote, "commit", "-q", "-m", "init")
	return remote
}

func TestRepoName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/u/magi-foo":      "magi-foo",
		"https://github.com/u/magi-foo.git":  "magi-foo",
		"https://github.com/u/magi-foo.git/": "magi-foo",
		"git@github.com:u/magi-bar.git":      "magi-bar",
		"magi-baz":                           "magi-baz",
		"https://x/u/magi-q.git?ref=main":    "magi-q", // query stripped
		"https://x/u/magi-s//":               "magi-s", // double trailing slash
	}
	for in, want := range cases {
		if got := repoName(in); got != want {
			t.Errorf("repoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscoverMarksGitAndSkipsNonPlugin(t *testing.T) {
	haveGit(t)
	root := t.TempDir()

	// (1) a git-managed plugin (cloned from a remote → has origin)
	remote := makeRemote(t, "gitplug", "1.0.0")
	run(t, root, "clone", "-q", remote, filepath.Join(root, "gitplug"))

	// (2) a hand-dropped plugin (plugin.toml, no .git)
	man := filepath.Join(root, "manualplug")
	if err := os.MkdirAll(man, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, man, "manualplug", "0.1.0")

	// (3) a directory that is not a plugin (no plugin.toml) → ignored
	if err := os.MkdirAll(filepath.Join(root, "notaplugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := Discover([]string{root})
	byName := map[string]Managed{}
	for _, m := range got {
		byName[m.Name] = m
	}
	if len(got) != 2 {
		t.Fatalf("want 2 discovered plugins, got %d: %+v", len(got), got)
	}
	g := byName["gitplug"]
	if !g.Git || g.Source == "" || g.Version != "1.0.0" {
		t.Errorf("gitplug = %+v, want Git=true, non-empty Source, Version 1.0.0", g)
	}
	m := byName["manualplug"]
	if m.Git || m.Source != "" {
		t.Errorf("manualplug = %+v, want Git=false, empty Source", m)
	}
}

func TestUpdateOneFastForwards(t *testing.T) {
	haveGit(t)
	root := t.TempDir()
	remote := makeRemote(t, "p", "1.0.0")
	dir := filepath.Join(root, "p")
	run(t, root, "clone", "-q", remote, dir)

	m := Discover([]string{root})[0]

	// No upstream change yet → up to date, not "Updated".
	if r := UpdateOne(context.Background(), m); r.Updated || r.Skipped != "" {
		t.Fatalf("expected up-to-date no-op, got %+v", r)
	}

	// Advance the remote (a new patch release), then update should fast-forward.
	writeManifest(t, remote, "p", "1.0.1")
	run(t, remote, "add", "-A")
	run(t, remote, "commit", "-q", "-m", "bump")

	r := UpdateOne(context.Background(), m)
	if !r.Updated || r.Skipped != "" || r.From == r.To {
		t.Fatalf("expected fast-forward update, got %+v", r)
	}
	// The working tree now reflects the new manifest.
	if md, _ := readManifest(dir); md.Version != "1.0.1" {
		t.Errorf("after update manifest version = %q, want 1.0.1", md.Version)
	}
}

func TestUpdateOneNonGitSkipped(t *testing.T) {
	m := Managed{Name: "manual", Dir: t.TempDir(), Git: false}
	r := UpdateOne(context.Background(), m)
	if r.Updated || r.Skipped == "" {
		t.Fatalf("non-git plugin must be skipped, got %+v", r)
	}
}

func TestUpdateOneRefusesNonFastForward(t *testing.T) {
	haveGit(t)
	root := t.TempDir()
	remote := makeRemote(t, "p", "1.0.0")
	dir := filepath.Join(root, "p")
	run(t, root, "clone", "-q", remote, dir)
	m := Discover([]string{root})[0]

	// Diverge: local commit AND remote commit on the same branch → not ff-able.
	writeManifest(t, dir, "p", "1.0.0-local")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "local")
	writeManifest(t, remote, "p", "1.0.1")
	run(t, remote, "add", "-A")
	run(t, remote, "commit", "-q", "-m", "remote")

	r := UpdateOne(context.Background(), m)
	if r.Updated || r.Skipped == "" {
		t.Fatalf("divergent history must be skipped (not force-reset), got %+v", r)
	}
	// Local commit preserved (not overwritten).
	if md, _ := readManifest(dir); md.Version != "1.0.0-local" {
		t.Errorf("local change was clobbered: manifest = %q", md.Version)
	}
}

func TestInstallClonesPlugin(t *testing.T) {
	haveGit(t)
	remote := makeRemote(t, "installed", "2.3.4")
	destRoot := t.TempDir()

	m, err := Install(context.Background(), remote, "", destRoot)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !m.Git || m.Version != "2.3.4" || m.Name != "installed" {
		t.Errorf("installed = %+v, want Git, Version 2.3.4, Name installed", m)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "plugin.toml")); err != nil {
		t.Errorf("plugin.toml missing after install: %v", err)
	}

	// A second install into the same root must refuse rather than clobber.
	if _, err := Install(context.Background(), remote, "", destRoot); err == nil {
		t.Error("expected refusal to overwrite an existing plugin dir")
	}
}

// Install honors a pin to a tag AND to a bare commit SHA (the latter needs the
// full-clone fallback, since `clone --branch` rejects a SHA).
func TestInstallAtPinnedRef(t *testing.T) {
	haveGit(t)
	remote := makeRemote(t, "pinned", "1.0.0")
	v1sha := strings.TrimSpace(run(t, remote, "rev-parse", "HEAD"))
	run(t, remote, "tag", "v1")
	// Advance the remote so HEAD != the pin; the pin must win.
	writeManifest(t, remote, "pinned", "2.0.0")
	run(t, remote, "add", "-A")
	run(t, remote, "commit", "-q", "-m", "bump")

	// Pin to the tag.
	tagDest := t.TempDir()
	m, err := Install(context.Background(), remote, "v1", tagDest)
	if err != nil {
		t.Fatalf("install @tag: %v", err)
	}
	if m.Version != "1.0.0" {
		t.Errorf("tag pin resolved to version %q, want 1.0.0", m.Version)
	}

	// Pin to the raw commit SHA (exercises the full-clone + checkout fallback).
	shaDest := t.TempDir()
	m2, err := Install(context.Background(), remote, v1sha, shaDest)
	if err != nil {
		t.Fatalf("install @sha: %v", err)
	}
	if m2.Version != "1.0.0" {
		t.Errorf("sha pin resolved to version %q, want 1.0.0", m2.Version)
	}
}

func TestInstallRejectsNonPluginRepo(t *testing.T) {
	haveGit(t)
	// A git repo with no plugin.toml is not a magi plugin.
	remote := t.TempDir()
	run(t, remote, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, remote, "add", "-A")
	run(t, remote, "commit", "-q", "-m", "init")

	destRoot := t.TempDir()
	if _, err := Install(context.Background(), remote, "", destRoot); err == nil {
		t.Fatal("expected error installing a repo without plugin.toml")
	}
	// And it must not leave a partial checkout behind.
	if _, err := os.Stat(filepath.Join(destRoot, filepath.Base(remote))); !os.IsNotExist(err) {
		t.Error("failed install should not leave a directory behind")
	}
}
