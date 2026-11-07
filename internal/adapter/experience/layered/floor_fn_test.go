package layered

import (
	"context"
	"testing"
)

// The floor is asked at retrieval time, so a model switched while running gets its own floor.
func TestTheFloorFunctionIsAskedOnEveryRetrieval(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "weak.md", "the mascot is a penguin")
	emb := (&ruleEmbedder{}).add("penguin", 0.3, 0.95394, 0).add("billing", 1, 0, 0)
	floor := 0.0
	s := New(dir, "", "").WithEmbedder(emb).WithMinSimilarityFunc(func() float64 { return floor })
	get := func() int {
		m, _, err := s.Retrieve(context.Background(), "billing", nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(m)
	}
	if n := get(); n != 1 {
		t.Fatalf("floor 0: the 0.3 match should rank, got %d", n)
	}
	floor = 0.5
	if n := get(); n != 0 {
		t.Fatalf("floor 0.5 set after construction should drop the 0.3 match, got %d", n)
	}
	floor = 7
	if n := get(); n != 1 {
		t.Fatalf("an out-of-range floor should count as 0, got %d", n)
	}
}
