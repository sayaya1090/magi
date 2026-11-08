package git

import (
	"math"
	"strings"

	"github.com/sayaya1090/magi/internal/core/lexical"
)

// RankCJK ranks candidates using unified BM25 over the visible corpus.
// It applies to all scripts, English, and code identifiers while preserving
// exact-match bonuses (such as ID matches >= 1000).
func RankCJK[T any](query string, candidates []Scored[T], textOf func(T) string) {
	if strings.TrimSpace(query) == "" || len(candidates) == 0 {
		return
	}
	docs := make([]string, len(candidates))
	for i, candidate := range candidates {
		docs[i] = textOf(candidate.V)
	}
	for i, score := range lexical.Scores(query, docs) {
		bonus := 0
		if candidates[i].Score >= 1000 {
			bonus = candidates[i].Score
		}
		candidates[i].Score = bonus
		if score > 0 {
			candidates[i].Score += max(1, int(math.Round(score*1000)))
		}
	}
}
