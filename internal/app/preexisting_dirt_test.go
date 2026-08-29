package app

import (
	"strings"
	"testing"
)

// The council's disclaimer names the paths that were dirty before the turn — the fact whose
// absence made three members order a pre-turn edit reverted as a violation.
func TestPreexistingDirtBannerNamesThePaths(t *testing.T) {
	if got := preexistingDirtBanner(nil); got != "" {
		t.Fatalf("a clean start needs no disclaimer, got %q", got)
	}
	got := preexistingDirtBanner([]string{"README.md", "docs/a.md"})
	for _, want := range []string{"ALREADY MODIFIED BEFORE THIS TURN", "README.md", "docs/a.md", "not\nevidence of this turn's work"} {
		if !strings.Contains(got, strings.ReplaceAll(want, "\n", " ")) && !strings.Contains(got, want) {
			t.Errorf("banner missing %q:\n%s", want, got)
		}
	}
	many := make([]string, 30)
	for i := range many {
		many[i] = "f.go"
	}
	if got := preexistingDirtBanner(many); !strings.Contains(got, "…") {
		t.Error("an uncapped list would drown the evidence")
	}
}

func TestPreexistingDirtPathsSwallowsNonCheckouts(t *testing.T) {
	if got := preexistingDirtPaths(GitState{}, nil); len(got) != 0 {
		t.Fatalf("no changes, no paths: %v", got)
	}
	if got := preexistingDirtPaths(GitState{Changes: []GitChange{{Path: "a"}, {Path: "b"}}}, nil); len(got) != 2 {
		t.Fatalf("want both paths, got %v", got)
	}
}
