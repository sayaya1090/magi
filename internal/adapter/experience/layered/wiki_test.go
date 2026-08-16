package layered

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A wiki page's default tier is TEAM: it is written to be read by the other companions, and a
// workspace-local default would file it where they never look. An explicit scope still wins, and
// a companion with no team falls back to the project rather than losing the page.
func TestAWikiPageDefaultsToTheTeamTier(t *testing.T) {
	ctx := context.Background()
	proj, team := t.TempDir(), t.TempDir()
	s := New(proj, team, "")

	edit := port.Contribution{Wiki: []port.WikiEdit{{Page: "auth flow", Text: "sidecar refreshes"}}, Source: "m"}
	if err := s.Propose(ctx, edit); err != nil {
		t.Fatal(err)
	}
	pages, _ := s.WikiSearch(ctx, "auth flow", 3)
	if len(pages) != 1 || pages[0].Tier != "team" {
		t.Fatalf("a default-scope page must land in the team tier, got %+v", pages)
	}

	// Explicit project scope routes like anything else.
	edit2 := port.Contribution{Wiki: []port.WikiEdit{{Page: "local quirk", Text: "this repo only"}}, Source: "m", Scope: "project"}
	if err := s.Propose(ctx, edit2); err != nil {
		t.Fatal(err)
	}
	pages, _ = s.WikiSearch(ctx, "local quirk", 3)
	if len(pages) != 1 || pages[0].Tier != "project" {
		t.Fatalf("an explicit project page must land there, got %+v", pages)
	}

	// No team declared: the page falls back to the project instead of vanishing.
	solo := New(proj, "", "")
	if err := solo.Propose(ctx, port.Contribution{Wiki: []port.WikiEdit{{Page: "fallback", Text: "kept"}}}); err != nil {
		t.Fatal(err)
	}
	pages, _ = solo.WikiSearch(ctx, "fallback", 3)
	if len(pages) != 1 || pages[0].Tier != "project" {
		t.Fatalf("with no team tier the page must fall back to project, got %+v", pages)
	}
}

// A memory keeps its old default: the wiki's team default must not drag ordinary remembering out
// of the workspace.
func TestAMemoryStillDefaultsToTheProjectTier(t *testing.T) {
	ctx := context.Background()
	proj, team := t.TempDir(), t.TempDir()
	s := New(proj, team, "")
	if err := s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "gateway timeout is 8s"}}}); err != nil {
		t.Fatal(err)
	}
	mems, _, _ := s.Retrieve(ctx, "gateway timeout", nil)
	if len(mems) != 1 || !contains(mems[0].ID, "[project]") {
		t.Fatalf("a default-scope memory must stay in the project tier, got %+v", mems)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
