package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// setup writes one companion's workspace and publishes it, so join reads exactly what a real one
// would: a record on disk and a project config beside it.
func publishWorkspace(t *testing.T, cfgDir, sid string, id daemon.Identity, projectTOML string) string {
	t.Helper()
	wd, err := os.MkdirTemp("/tmp", "magijoin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(wd) })
	if projectTOML != "" {
		if err := os.MkdirAll(filepath.Join(wd, ".magi"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wd, ".magi", "config.toml"), []byte(projectTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(cfgDir, "daemon-"+sid+".sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unpublish, err := daemon.Publish(sock, wd, sid, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpublish)
	return wd
}

// A newcomer starts knowing nothing the team agreed on. Joining says what they share — and applies
// none of it, because an [mcp] entry is a command this process would later run.
func TestJoinProposesWhatTheTeamSharesAndAppliesNothing(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoincfg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)

	// experience_dir before the first table: after it, TOML reads it as companion.experience_dir.
	publishWorkspace(t, cfg, "d", daemon.Identity{Name: "design", Team: "frontend"}, `
experience_dir = "/srv/team-experience"

[companion]
name = "design"
team = "frontend"
role = "the design system"

[mcp.figma]
command = "npx"
args = ["-y", "figma-mcp"]
env = ["FIGMA_TOKEN=secret-value-nobody-should-copy"]

[[hooks]]
event = "PostToolUse"
command = "true"
`)
	mine, err := os.MkdirTemp("/tmp", "magijoinme")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "design"); code != 0 {
		t.Fatalf("join exited %d: %s", code, out.String())
	}

	// Nothing applied: the workspace's own config is still absent.
	if _, err := os.Stat(filepath.Join(mine, ".magi", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("joining wrote the live config")
	}
	b, err := os.ReadFile(filepath.Join(mine, ".magi", "joined-design.toml"))
	if err != nil {
		t.Fatalf("no proposal was written: %v", err)
	}
	got := string(b)

	// The one line that makes a newcomer start knowing things.
	if !strings.Contains(got, `experience_dir = "/srv/team-experience"`) {
		t.Errorf("the shared brain is missing:\n%s", got)
	}
	if !strings.Contains(got, "team = \"frontend\"") {
		t.Errorf("the team is missing:\n%s", got)
	}
	// The MCP server is named and described, and commented out — a command is a decision.
	if !strings.Contains(got, "[mcp.figma]") || !strings.Contains(got, `command = "npx"`) {
		t.Errorf("the MCP server is missing:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if line != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") &&
			!strings.HasPrefix(strings.TrimSpace(line), "[companion]") {
			t.Errorf("a live setting is in the proposal: %q", line)
		}
	}
	// A token copied into a second workspace is a token in two places.
	if strings.Contains(got, "secret-value-nobody-should-copy") {
		t.Error("an env VALUE was copied across")
	}
	if !strings.Contains(got, "FIGMA_TOKEN") {
		t.Errorf("the env var is not even named:\n%s", got)
	}
	// Hooks are shell lines: named, counted, never copied.
	if !strings.Contains(got, "1 hook(s)") || strings.Contains(got, `command = "true"`) {
		t.Errorf("hooks were handled wrongly:\n%s", got)
	}
	// And the person is told nothing happened.
	if !strings.Contains(out.String(), "Nothing has been applied") {
		t.Errorf("the message does not say it applied nothing: %s", out.String())
	}
}

// Joining a name nobody has answers with who is there — the next thing anybody does is name one.
func TestJoinNamesWhoIsThereWhenNobodyMatches(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoincfg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)
	publishWorkspace(t, cfg, "d", daemon.Identity{Name: "design", Team: "frontend"}, "")
	mine := t.TempDir()

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "database"); code != 1 {
		t.Fatalf("joining nobody exited %d", code)
	}
	if !strings.Contains(out.String(), "design [frontend]") {
		t.Errorf("the message does not say who is there: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(mine, ".magi")); !os.IsNotExist(err) {
		t.Error("a failed join left files behind")
	}
}

// A team name matching several is the ordinary case, and the newcomer has to say whose setup it
// means to copy.
func TestJoiningATeamOfSeveralAsksWhichOne(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoincfg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)
	publishWorkspace(t, cfg, "a", daemon.Identity{Name: "design", Team: "frontend"}, "")
	publishWorkspace(t, cfg, "b", daemon.Identity{Name: "buttons", Team: "frontend"}, "")

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, t.TempDir(), "frontend"); code != 1 {
		t.Fatalf("exited %d: %s", code, out.String())
	}
	for _, want := range []string{"design", "buttons"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the message does not name %q: %s", want, out.String())
		}
	}
}

// A companion that shares nothing says so, rather than handing over an empty file that reads as a
// failure.
func TestJoiningSomebodyWhoSharesNothingSaysSo(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoincfg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)
	publishWorkspace(t, cfg, "s", daemon.Identity{Name: "solo"}, "[companion]\nname = \"solo\"\n")
	mine, err := os.MkdirTemp("/tmp", "magijoinme")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "solo"); code != 0 {
		t.Fatalf("exited %d: %s", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(mine, ".magi", "joined-solo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "share nothing beyond this") {
		t.Errorf("an empty proposal does not say it is empty:\n%s", b)
	}
}
