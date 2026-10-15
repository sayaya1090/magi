package git

import (
	"github.com/sayaya1090/magi/internal/core/lexical"
	"math"
)

// RankCJK preserves the legacy English scorer. Layered retrieval calls this
// again over the complete visible union so scores share corpus statistics.
func RankCJK[T any](query string, candidates []Scored[T], textOf func(T) string) {
	if !lexical.HasCJK(query) {
		return
	}
	docs := make([]string, len(candidates))
	for i, candidate := range candidates {
		docs[i] = textOf(candidate.V)
	}
	for i, score := range lexical.Scores(query, docs) {
		candidates[i].Score = 0
		if score > 0 {
			candidates[i].Score = max(1, int(math.Round(score*1000)))
		}
	}
}
