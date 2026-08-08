package layered

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func seedMem(t *testing.T, dir, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memories", file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Retrieve merges both tiers under one budget and tags each entry with its tier.
func TestRetrieveMergesAndTags(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	seedMem(t, projDir, "p.md", "deploy uses the staging cluster first")
	seedMem(t, globDir, "g.md", "deploy scripts always run gofmt")

	s := New(projDir, "", globDir)
	mems, _, err := s.Retrieve(context.Background(), "how does deploy work?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("want 2 merged memories, got %d: %+v", len(mems), mems)
	}
	// Project entry comes first (most context-specific) and both carry a tier tag.
	if !strings.HasPrefix(mems[0].Text, "[project]") {
		t.Errorf("first entry should be the project tier, got %q", mems[0].Text)
	}
	var sawGlobal bool
	for _, m := range mems {
		if strings.HasPrefix(m.Text, "[global]") {
			sawGlobal = true
		}
	}
	if !sawGlobal {
		t.Errorf("global tier missing from merge: %+v", mems)
	}
}

// Retrieve caps the merged result so adding a tier never widens injected context.
func TestRetrieveCombinedCap(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	for i := 0; i < 6; i++ {
		seedMem(t, projDir, string(rune('a'+i))+".md", "cache invalidation strategy note")
		seedMem(t, globDir, string(rune('a'+i))+".md", "cache invalidation strategy note")
	}
	s := New(projDir, "", globDir)
	mems, _, err := s.Retrieve(context.Background(), "cache invalidation strategy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) > 5 {
		t.Fatalf("combined cap should hold merged memories at 5, got %d", len(mems))
	}
}

// Propose routes by scope, defaulting to the project tier.
func TestProposeScopeRouting(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	s := New(projDir, "", globDir)
	ctx := context.Background()

	if err := s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "project-scoped default"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Propose(ctx, port.Contribution{Scope: "global", Memories: []port.Memory{{Text: "global-scoped"}}}); err != nil {
		t.Fatal(err)
	}
	if n := countMem(projDir); n != 1 {
		t.Errorf("default scope should write to project tier, project has %d files", n)
	}
	if n := countMem(globDir); n != 1 {
		t.Errorf("global scope should write to global tier, global has %d files", n)
	}
}

func countMem(dir string) int {
	entries, _ := os.ReadDir(filepath.Join(dir, "memories"))
	return len(entries)
}

// The team tier is what a workspace and a machine cannot hold between them.
//
// A project tier is one companion's directory and a global tier is every companion on the machine.
// Neither can carry "the frontend team decided X" — the thing somebody wants to write once and have
// three companions follow — so it sits between them, more specific than the machine and less than
// one directory.
func TestTheTeamTierIsItsOwnPlace(t *testing.T) {
	dirs := [3]string{t.TempDir(), t.TempDir(), t.TempDir()}
	s := New(dirs[0], dirs[1], dirs[2])
	ctx := context.Background()

	for i, scope := range []string{"project", "team", "global"} {
		if err := s.Propose(ctx, port.Contribution{
			Memories: []port.Memory{{Text: "a fact for " + scope + " about widgets"}},
			Scope:    scope,
		}); err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		if n := len(readDirNames(t, dirs[i])); n == 0 {
			t.Errorf("%s went somewhere other than its own tier", scope)
		}
	}

	// And all three come back, tagged, so a reader can tell whose knowledge it is.
	mems, _, err := s.Retrieve(ctx, "widgets", nil)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, m := range mems {
		seen = append(seen, m.Text)
	}
	joined := strings.Join(seen, " | ")
	for _, tier := range []string{"[project]", "[team]", "[global]"} {
		if !strings.Contains(joined, tier) {
			t.Errorf("%s is missing from a retrieval that should merge all three: %s", tier, joined)
		}
	}
}

// A companion with no team must not lose what it was asked to remember for one.
//
// It falls to the project rather than to the machine: writing a team decision into the global tier
// would put it in front of every unrelated companion on the box.
func TestRememberingForATeamThatDoesNotExistFallsToTheProject(t *testing.T) {
	proj, glob := t.TempDir(), t.TempDir()
	s := New(proj, "", glob)
	if err := s.Propose(context.Background(), port.Contribution{
		Memories: []port.Memory{{Text: "something for a team this companion is not on"}},
		Scope:    "team",
	}); err != nil {
		t.Fatal(err)
	}
	if len(readDirNames(t, proj)) == 0 {
		t.Error("it was dropped instead of landing in the project tier")
	}
	if len(readDirNames(t, glob)) != 0 {
		t.Error("it landed in the global tier, in front of every companion on the machine")
	}
}

func readDirNames(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && filepath.Ext(p) == ".md" {
			out = append(out, p)
		}
		return nil
	})
	return out
}
