package layered

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
)

// A long memory found by meaning carries the passage that matched, not its opening.
func TestARecalledMemoryCarriesItsMatchingPassage(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "long.md", strings.Repeat("filler ", 157)+"\n\n"+strings.Repeat("zebra stripes ", 110))
	emb := (&ruleEmbedder{}).add("filler", 0, 1, 0).add("zebra", 1, 0, 0).add("equine", 1, 0, 0)
	mems, _, err := New(dir, "", "").WithEmbedder(emb).WithMinSimilarity(0.5).Retrieve(context.Background(), "equine pattern", nil)
	if err != nil || len(mems) == 0 {
		t.Fatalf("no memory: %v", err)
	}
	e := mems[0].Excerpt
	if !strings.Contains(e, "zebra") || strings.Contains(e, "filler") {
		t.Fatalf("excerpt should be the zebra passage only, got %q", e)
	}
	if n := utf8.RuneCountInString(e); n > excerptRunes+1 {
		t.Fatalf("excerpt has %d runes, over %d", n, excerptRunes)
	}
	if !strings.Contains(mems[0].Text, "filler") {
		t.Fatal("the full text must stay intact for recall_memory")
	}
}

// Lexical ranking gives no passage-level evidence, so it sets no excerpt.
func TestLexicalRecallSetsNoExcerpt(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "a.md", "the invoice job runs nightly")
	mems, _, err := New(dir, "", "").Retrieve(context.Background(), "invoice", nil)
	if err != nil || len(mems) != 1 {
		t.Fatalf("got %v %v", mems, err)
	}
	if mems[0].Excerpt != "" {
		t.Fatalf("lexical recall should not set an excerpt, got %q", mems[0].Excerpt)
	}
}

// The field is retrieval-only: a stored memory without it serializes exactly as before, so managed
// records and their operation digests do not change.
func TestAnEmptyExcerptIsNotSerialized(t *testing.T) {
	b, err := json.Marshal(port.Memory{ID: "x", Text: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"ID":"x","Text":"y","Tags":null}` {
		t.Fatalf("stored form changed: %s", b)
	}
}
