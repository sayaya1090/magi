package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

type fakeWikiExp struct {
	port.ExperienceStore
	pages []port.WikiPage
}

func (f *fakeWikiExp) Retrieve(ctx context.Context, q string, g []string) ([]port.Memory, []port.Skill, error) {
	return nil, nil, nil
}
func (f *fakeWikiExp) Propose(ctx context.Context, c port.Contribution) error { return nil }
func (f *fakeWikiExp) WikiSearch(ctx context.Context, q string, n int) ([]port.WikiPage, error) {
	return f.pages, nil
}
func (f *fakeWikiExp) WikiIndex(ctx context.Context, n int) ([]port.WikiPage, error) {
	return f.pages, nil
}

// The advisory fires when a NEW title half-overlaps an existing page — and stays silent for an
// update to an existing title, because nagging every legitimate update teaches the model to
// ignore the note.
func TestWikiNeighborNoteAdvisesNewTitlesOnly(t *testing.T) {
	a := &App{cfg: Config{Experience: &fakeWikiExp{pages: []port.WikiPage{{Title: "auth flow"}}}}}
	ctx := context.Background()

	if n := a.wikiNeighborNote(ctx, "auth flow details"); !strings.Contains(n, "auth flow") {
		t.Errorf("a half-overlapping new title must be advised, got %q", n)
	}
	if n := a.wikiNeighborNote(ctx, "auth flow"); n != "" {
		t.Errorf("an update to the exact existing title must not be nagged, got %q", n)
	}
	if n := a.wikiNeighborNote(ctx, "deploy pipeline"); n != "" {
		t.Errorf("an unrelated title must not be advised, got %q", n)
	}
}
