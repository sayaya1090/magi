package app

import (
	"strings"
	"testing"
)

// reviewGroups is what makes the review gate fan out proportionally to the change:
// one reviewer per changed *area*, splitting an area that exceeds reviewGroupMaxFiles.
// These cases pin the partitioning so a regression can't silently collapse the
// fan-out back to a single shallow reviewer.
func TestReviewGroupsPartitioning(t *testing.T) {
	t.Run("empty change yields one working-tree group", func(t *testing.T) {
		g := reviewGroups(nil, reviewGroupMaxFiles)
		if len(g) != 1 || g[0].label != "(working tree)" || len(g[0].files) != 0 {
			t.Fatalf("want a single empty (working tree) group, got %+v", g)
		}
	})

	t.Run("groups by directory, preserving first-seen order", func(t *testing.T) {
		changes := []fileChange{
			{path: "internal/app/a.go", before: "x"},
			{path: "internal/core/b.go", before: "x"},
			{path: "internal/app/c.go", before: ""}, // new
		}
		g := reviewGroups(changes, reviewGroupMaxFiles)
		if len(g) != 2 {
			t.Fatalf("want 2 dir groups, got %d: %+v", len(g), g)
		}
		if g[0].label != "internal/app" || g[1].label != "internal/core" {
			t.Fatalf("want [internal/app, internal/core] in first-seen order, got [%s, %s]", g[0].label, g[1].label)
		}
		if len(g[0].files) != 2 {
			t.Fatalf("internal/app group should hold both its files, got %v", g[0].files)
		}
		// New files are marked so the reviewer knows they are additions.
		if !strings.Contains(strings.Join(g[0].files, "\n"), "internal/app/c.go (new)") {
			t.Fatalf("new file should be marked (new): %v", g[0].files)
		}
	})

	t.Run("root-level files get a stable label", func(t *testing.T) {
		g := reviewGroups([]fileChange{{path: "main.go", before: "x"}}, reviewGroupMaxFiles)
		if len(g) != 1 || g[0].label != "(root)" {
			t.Fatalf("want a single (root) group, got %+v", g)
		}
	})

	t.Run("an oversized area splits into capped slices", func(t *testing.T) {
		var changes []fileChange
		for i := 0; i < reviewGroupMaxFiles*2+1; i++ { // 2 full slices + 1
			changes = append(changes, fileChange{path: "pkg/f" + string(rune('a'+i)) + ".go", before: "x"})
		}
		g := reviewGroups(changes, reviewGroupMaxFiles)
		if len(g) != 3 {
			t.Fatalf("want 3 slices for %d files at cap %d, got %d", len(changes), reviewGroupMaxFiles, len(g))
		}
		for i, grp := range g {
			if len(grp.files) > reviewGroupMaxFiles {
				t.Fatalf("slice %d exceeds cap: %d files", i, len(grp.files))
			}
		}
		// Later slices are distinguished so their reviewer notes don't collide.
		if g[0].label != "pkg" || !strings.HasPrefix(g[1].label, "pkg [") {
			t.Fatalf("later slices should carry a distinct label, got [%s, %s]", g[0].label, g[1].label)
		}
	})
}
