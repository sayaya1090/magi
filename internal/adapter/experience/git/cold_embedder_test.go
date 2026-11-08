package git

import (
	"context"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// coldEmbedder is up and has cached nothing yet, which is every embedder's first minutes.
type coldEmbedder struct{ modelEmbedder }

func (*coldEmbedder) Peekable() bool                    { return true }
func (*coldEmbedder) Peek([]string) ([][]float32, bool) { return nil, false }
func (*coldEmbedder) Warm([]string)                     {}

func seedColdCorpus(t *testing.T, s *Store) {
	t.Helper()
	texts := []string{
		"sqlite busy timeout retry driver wal checkpoint contention lock",
		"kubernetes ingress annotation rewrite target path",
		"gradle wrapper distribution checksum verification",
		"typescript strict null checks compiler option",
		"postgres vacuum analyze autovacuum threshold",
		"docker layer cache invalidation copy order",
	}
	for i, text := range texts {
		if _, err := s.ApplyExperience(context.Background(), port.ExperienceChange{
			ObjectID: fmt.Sprintf("mem_%d", i), OperationID: fmt.Sprintf("op_%d", i), ObservationID: fmt.Sprintf("obs_%d", i),
			Owner: "plugin:engram", Source: "plugin:engram", Kind: "memory", State: "active", HostOutcome: "verified",
			Memory: &port.Memory{Text: text},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// While the cache fills, every similarity is 0 because nothing was measured. Reading that as
// "unrelated" hid the memory a new lesson repeats, and the duplicate was then written beside it.
func TestColdEmbedderJudgesLikeNoEmbedderAtTheStrictGate(t *testing.T) {
	const query = "sqlite busy timeout retry driver wal checkpoint contention lock"
	ctx := context.Background()
	plain := New(t.TempDir())
	seedColdCorpus(t, plain)
	cold := New(t.TempDir()).WithSame(&coldEmbedder{modelEmbedder{model: "m"}})
	seedColdCorpus(t, cold)
	for _, minSim := range []float64{0.20, 0.50, 0.78} {
		want, err := plain.SearchManagedExperience(ctx, query, "memory", minSim, 5)
		if err != nil {
			t.Fatal(err)
		}
		got, err := cold.SearchManagedExperience(ctx, query, "memory", minSim, 5)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("minSim=%.2f plain=%d cold=%d", minSim, len(want), len(got))
		if len(got) != len(want) {
			t.Fatalf("minSim %.2f: cold embedder found %d, no embedder %d", minSim, len(got), len(want))
		}
		if len(want) > 0 && got[0].ObjectID != want[0].ObjectID {
			t.Fatalf("minSim %.2f: cold embedder ranked %s first, no embedder %s", minSim, got[0].ObjectID, want[0].ObjectID)
		}
	}
	if hit, err := cold.SearchManagedExperience(ctx, query, "memory", 0.78, 5); err != nil || len(hit) == 0 || hit[0].ObjectID != "mem_0" {
		t.Fatalf("an exact repeat should still be found at the strict gate: %v %v", hit, err)
	}
}
