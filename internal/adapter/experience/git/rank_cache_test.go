package git

import (
	"context"
	"fmt"
	"testing"
)

type modelEmbedder struct {
	model string
	calls int
}

func (e *modelEmbedder) Available() bool      { return true }
func (e *modelEmbedder) CurrentModel() string { return e.model }
func (e *modelEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(len(e.model))}
	}
	return out, nil
}

func TestRankCacheDoesNotServeAnotherModelsVectors(t *testing.T) {
	e := &modelEmbedder{model: "m"}
	chunks, query := []string{"rank-cache-model-chunk"}, "rank-cache-model-query"
	if _, err := embedForRank(context.Background(), e, chunks, query); err != nil {
		t.Fatal(err)
	}
	if _, err := embedForRank(context.Background(), e, chunks, query); err != nil {
		t.Fatal(err)
	}
	if e.calls != 1 {
		t.Fatalf("same model should hit the cache, embedded %d times", e.calls)
	}
	e.model = "longer-model"
	got, err := embedForRank(context.Background(), e, chunks, query)
	if err != nil {
		t.Fatal(err)
	}
	if e.calls != 2 || got[0][0] != float32(len(e.model)) {
		t.Fatalf("a new model got old vectors: calls=%d vec=%v", e.calls, got[0])
	}
}

func TestRankCacheAtItsLimitKeepsWhatItCan(t *testing.T) {
	rankVecMu.Lock()
	saved := rankVecCache
	rankVecCache = make(map[string][]float32, rankVecCap)
	for i := 0; i < rankVecCap; i++ {
		rankVecCache[fmt.Sprintf("old-%d", i)] = []float32{1}
	}
	rankVecMu.Unlock()
	t.Cleanup(func() {
		rankVecMu.Lock()
		rankVecCache = saved
		rankVecMu.Unlock()
	})
	storeRankVecs([]string{"new-a", "new-b"}, [][]float32{{2}, {3}})
	rankVecMu.RLock()
	defer rankVecMu.RUnlock()
	if len(rankVecCache) != rankVecCap {
		t.Fatalf("cache holds %d, want %d", len(rankVecCache), rankVecCap)
	}
	if rankVecCache["new-a"] == nil || rankVecCache["new-b"] == nil {
		t.Fatal("the new vectors were not stored")
	}
}
