package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestExperienceExcerptsListsOnlyMemoriesWithAPassage(t *testing.T) {
	if got := experienceExcerpts([]port.Memory{{ID: "a", Text: "t"}}); got != "" {
		t.Fatalf("no excerpt should render nothing, got %q", got)
	}
	got := experienceExcerpts([]port.Memory{
		{ID: "[project] a", Text: "long", Excerpt: "line one\n  line two"},
		{ID: "[project] b", Text: "short"},
	})
	want := "Matching passages (recall_memory returns the full text):\n- [project] a: line one line two"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
