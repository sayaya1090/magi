package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestContextRetrievalBaselineOrderAndWhitespace(t *testing.T) {
	a := &App{contextProviders: []port.ContextProvider{
		&fakeProvider{chunks: []port.ContextChunk{{Source: "same", Text: "  첫째 \n"}, {Source: "same", Text: "둘째"}, {Text: "  "}}},
		&fakeProvider{chunks: []port.ContextChunk{{Text: "셋째"}}},
	}}
	got := a.gatherContext(context.Background(), port.ContextQuery{})
	want := "## same\n첫째\n\n## same\n둘째\n\n셋째"
	if got != want {
		t.Fatalf("context changed: got %q want %q", got, want)
	}
}
