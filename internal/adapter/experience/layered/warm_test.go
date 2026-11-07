package layered

import (
	"context"
	"strings"
	"testing"
)

// warmFake is an embedder with a cache it can be told is full or empty.
type warmFake struct {
	ruleEmbedder
	warm      bool
	warmCalls int
	embedded  int // texts sent to Embed
}

func (w *warmFake) Peekable() bool { return true }
func (w *warmFake) Peek(texts []string) ([][]float32, bool) {
	if !w.warm {
		return make([][]float32, len(texts)), false
	}
	v, _ := w.ruleEmbedder.Embed(context.Background(), texts)
	return v, true
}
func (w *warmFake) Warm([]string) { w.warmCalls++ }
func (w *warmFake) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	w.embedded += len(texts)
	return w.ruleEmbedder.Embed(ctx, texts)
}

// A cold cache does not hold the turn: retrieval starts a warm, embeds nothing, and answers with
// the lexical ranking. Once the cache is full the same query is ranked by meaning, and only the
// query is embedded in the turn.
func TestAColdCacheWarmsInTheBackgroundInsteadOfBlocking(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "invoice.md", "the invoice job runs nightly and retries twice")
	writeMemory(t, dir, "other.md", "the mascot is a penguin")
	f := &warmFake{}
	f.add("invoice", 1, 0, 0).add("billing", 1, 0, 0)
	s := New(dir, "", "").WithEmbedder(f).WithMinSimilarity(0.5)

	mems, _, err := s.Retrieve(context.Background(), "billing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 0 || f.embedded != 0 || f.warmCalls == 0 {
		t.Fatalf("cold: want lexical-only (none), no embedding, a warm started; got %d memories, %d embedded, %d warms", len(mems), f.embedded, f.warmCalls)
	}

	f.warm = true
	mems, _, err = s.Retrieve(context.Background(), "billing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) == 0 || !strings.Contains(mems[0].Text, "invoice") {
		t.Fatalf("warm: the invoice memory should be found by meaning; got %v", mems)
	}
	if f.embedded != 1 {
		t.Fatalf("warm: only the query should be embedded in the turn, got %d texts", f.embedded)
	}
}
