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
embed_model = "team-embed-v2"

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
	if !strings.Contains(string(b), "share nothing else") {
		t.Errorf("an empty proposal does not say it is empty:\n%s", b)
	}
	// It is not a blank page even so: mcp_peers is what joining a team is usually for, and it is
	// the one line that is a switch rather than a decision.
	if !strings.Contains(string(b), "mcp_peers") {
		t.Errorf("a newcomer is not told how to reach them at all:\n%s", b)
	}
}

// The embedding model comes with the join, because it is not a preference.
//
// Two companions searching one team's notes with different embedding models compare numbers from
// different spaces: the answer is not merely worse, it is meaningless, and the per-model vector
// cache means the newcomer silently re-embeds everything and shares no work with them. The
// ENDPOINT is not copied — it may name a host only their machine can reach, and a key is never
// copied by this file at all.
func TestJoinCarriesTheEmbeddingModelAndNotItsEndpoint(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoinembed")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)

	publishWorkspace(t, cfg, "d", daemon.Identity{Name: "design", Team: "frontend"}, `
embed_model = "team-embed-v2"
base_url = "http://their-box:11434/v1"
api_key = "sk-theirs"

[companion]
name = "design"
team = "frontend"
`)
	mine, err := os.MkdirTemp("/tmp", "magijoinembedme")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)
	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "design"); code != 0 {
		t.Fatalf("join exited %d: %s", code, out.String())
	}
	body, err := os.ReadFile(filepath.Join(mine, ".magi", "joined-design.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, `embed_model = "team-embed-v2"`) {
		t.Errorf("the model a newcomer has to match did not come across:\n%s", got)
	}
	if !strings.Contains(got, "MATCH") {
		t.Errorf("nothing says the model has to be the same one:\n%s", got)
	}
	// Their endpoint and their key stay theirs.
	for _, secret := range []string{"their-box", "sk-theirs"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q was copied into somebody else's workspace:\n%s", secret, got)
		}
	}
	// And it is a proposal like everything else here: commented out, in effect nowhere.
	if !strings.Contains(got, "# embed_model") {
		t.Errorf("the line is live rather than proposed:\n%s", got)
	}
}

// The proposal is one [companion] section, not two.
//
// It is written to be read as the thing it would become, and two tables of one name is not TOML.
// The mcp_peers paragraph and the team/name/role block are both about the same section, and the
// first attempt at this emitted a header for each.
func TestTheProposalHasOneCompanionSection(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoinsec")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)
	publishWorkspace(t, cfg, "s", daemon.Identity{Name: "design", Team: "frontend"},
		"[companion]\nname = \"design\"\nteam = \"frontend\"\n")
	mine, err := os.MkdirTemp("/tmp", "magijoinme2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "design"); code != 0 {
		t.Fatalf("exited %d: %s", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(mine, ".magi", "joined-design.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n[companion]"); n != 1 {
		t.Errorf("%d [companion] headers:\n%s", n, b)
	}
	// Both things it is about are inside it.
	for _, want := range []string{"team = \"frontend\"", "mcp_peers = true"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the section is missing %q:\n%s", want, b)
		}
	}
}

// A workspace whose only shared thing is an AGENTS.md is not "shares nothing".
//
// The old test for that read the LENGTH of the proposal rather than the config, so it answered
// "did we write much" — and the first paragraph added above it would have turned every such join
// into a page that lists their standing instructions and then says they share nothing.
func TestAnAgentsFileCountsAsSomethingShared(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magijoinag")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cfg)
	wd := publishWorkspace(t, cfg, "s", daemon.Identity{Name: "docs"}, "[companion]\nname = \"docs\"\n")
	if err := os.WriteFile(filepath.Join(wd, ".magi", "AGENTS.md"), []byte("# how we work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine, err := os.MkdirTemp("/tmp", "magijoinme3")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mine)

	var out bytes.Buffer
	if code := joinTeam(&out, cfg, mine, "docs"); code != 0 {
		t.Fatalf("exited %d: %s", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(mine, ".magi", "joined-docs.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "AGENTS.md") {
		t.Fatalf("their standing instructions are not mentioned:\n%s", b)
	}
	if strings.Contains(string(b), "share nothing else") {
		t.Errorf("it lists their AGENTS.md and then says they share nothing:\n%s", b)
	}
}
