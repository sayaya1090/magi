// Package rank orders documents against a query by how RARE the words they share are.
//
// # Why this is not a word count
//
// Counting shared words says a note mentioning "the" and "handler" matches a query about "the
// handler" as well as one mentioning "dehydration" matches a query about dehydration. It does not:
// the second is a real signal and the first is two of the commonest tokens in the corpus. Standard
// BM25 IDF is the fix — a token's weight shrinks toward zero as it saturates the set — and it costs
// one pass to compute.
//
// # Why it lives here
//
// It was written once for the context-recall hints and then wanted again the moment a companion
// could be asked what it knows. Two spellings of one ranking is the shape most of this tree's
// defects have taken: they agree on the day they are written and nobody notices when they stop.
//
// # What it is not
//
// It is not semantic. "invoice" does not match "billing" here, and no amount of IDF will make it —
// that needs an embedding, which needs a model, which is a dependency this package does not have.
// What it does is rank the lexical matches honestly, which is a different and cheaper thing, and
// the floor any semantic layer would be built on top of.
package rank

import (
	"math"
	"sort"
	"strings"
)

// Hit is one document that matched, with its score and how many distinct query tokens it carried.
type Hit struct {
	Index   int
	Score   float64
	Matched int
}

// Tokenize splits text into the words this package compares on.
//
// Three characters and up, lowercase, letters and digits only. Shorter tokens are almost all
// grammar — "a", "to", "of" — and matching on them makes every document match every query.
func Tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= 3 {
			out[w] = true
		}
	}
	return out
}

// ByIDF ranks every document that shares a word with the query, best first.
//
// It ranks; it does not decide what is good enough. Those are different jobs and the answer differs
// by caller: the context-recall hints want precision, because a wrong hint spends a step, and are
// strict about it (Hit.Matched >= 2). A search for what somebody wrote down wants recall, because
// the corpus is a few dozen notes and a query of one rare word is the normal case. A threshold
// baked in here made the second impossible — "dehydration handler" found nothing at all, because
// the one document about dehydration carried only one of the two tokens.
//
// Nothing is lost by ranking loosely: IDF already sends a match on a word everybody used to the
// bottom. Ties break on how many tokens matched, then on the original order, so the same inputs
// always give the same answer.
func ByIDF(query string, docs []string) []Hit {
	qtoks := Tokenize(query)
	if len(qtoks) == 0 || len(docs) == 0 {
		return nil
	}
	lower := make([]string, len(docs))
	df := map[string]int{}
	for i, d := range docs {
		lower[i] = strings.ToLower(d)
		for t := range qtoks {
			if strings.Contains(lower[i], t) {
				df[t]++
			}
		}
	}
	n := float64(len(docs))
	// idf(t) = log(1 + (N - df + 0.5)/(df + 0.5)). Always above zero, and shrinking as a token
	// approaches every document — a word everybody used says nothing about who to read.
	idf := func(t string) float64 {
		d := float64(df[t])
		return math.Log(1 + (n-d+0.5)/(d+0.5))
	}
	var hits []Hit
	for i := range docs {
		var score float64
		matched := 0
		for t := range qtoks {
			if strings.Contains(lower[i], t) {
				score += idf(t)
				matched++
			}
		}
		if matched > 0 {
			hits = append(hits, Hit{Index: i, Score: score, Matched: matched})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		return hits[a].Matched > hits[b].Matched
	})
	return hits
}
