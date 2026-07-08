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

	s := New(projDir, globDir)
	mems, _, err := s.Retrieve(context.Background(), "how does deploy work?")
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
	s := New(projDir, globDir)
	mems, _, err := s.Retrieve(context.Background(), "cache invalidation strategy")
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
	s := New(projDir, globDir)
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
