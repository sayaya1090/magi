package layered

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ruleEmbedder gives each text the vector of the first rule whose word it contains. Rules are
// checked in order, so a text holding both "filler" and "zebra" gets filler's vector: the way one
// averaged vector for a long document is pulled toward what most of it is about.
type ruleEmbedder struct {
	rules []struct {
		word string
		vec  []float32
	}
}

func (r *ruleEmbedder) Available() bool { return true }
func (r *ruleEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{0, 0, 1}
		for _, rule := range r.rules {
			if strings.Contains(t, rule.word) {
				out[i] = rule.vec
				break
			}
		}
	}
	return out, nil
}

func (r *ruleEmbedder) add(word string, vec ...float32) *ruleEmbedder {
	r.rules = append(r.rules, struct {
		word string
		vec  []float32
	}{word, vec})
	return r
}

func writeMemory(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memories", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The answer is in the last paragraph of a long memory whose first part is about something else.
// Embedded whole, the memory is one vector pulled toward the filler and the query misses it; split
// into passages, the last passage holds only the answer and is found. The query shares no word with
// the memory, so the lexical half cannot rescue it either.
func TestTheMatchingPassageOfALongMemoryIsFound(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("filler ", 157) + "\n\n" + strings.Repeat("zebra stripes ", 110)
	writeMemory(t, dir, "long.md", body)
	writeMemory(t, dir, "other.md", "the mascot is a penguin")

	emb := (&ruleEmbedder{}).add("filler", 0, 1, 0).add("zebra", 1, 0, 0).add("equine", 1, 0, 0)
	s := New(dir, "", "").WithEmbedder(emb).WithMinSimilarity(0.5)
	mems, _, err := s.Retrieve(context.Background(), "equine pattern", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) == 0 || !strings.Contains(mems[0].Text, "zebra") {
		t.Fatalf("the long memory should be found by its last passage; got %d memories", len(mems))
	}
}

// A floor keeps a weak semantic match out of the ranking. Without one, any positive cosine gets in.
func TestTheSimilarityFloorDropsAWeakMatch(t *testing.T) {
	dir := t.TempDir()
	writeMemory(t, dir, "near.md", "invoices are retried twice")
	writeMemory(t, dir, "weak.md", "the mascot is a penguin")
	emb := (&ruleEmbedder{}).add("invoices", 0.9, 0.43589, 0).add("penguin", 0.3, 0.95394, 0).add("billing", 1, 0, 0)

	get := func(s *Store) []string {
		mems, _, err := s.Retrieve(context.Background(), "billing", nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range mems {
			out = append(out, m.Text)
		}
		return out
	}
	if got := get(New(dir, "", "").WithEmbedder(emb)); len(got) != 2 {
		t.Fatalf("without a floor both positive matches rank; got %v", got)
	}
	got := get(New(dir, "", "").WithEmbedder(emb).WithMinSimilarity(0.5))
	if len(got) != 1 || !strings.Contains(got[0], "invoices") {
		t.Fatalf("with a 0.5 floor only the 0.9 match should rank; got %v", got)
	}
}

func TestAnOutOfRangeFloorIsIgnored(t *testing.T) {
	for _, v := range []float64{-0.1, 1, 2} {
		if s := New("", "", "").WithMinSimilarity(v); s.minSim != 0 {
			t.Fatalf("floor %v should be ignored, got %v", v, s.minSim)
		}
	}
}
