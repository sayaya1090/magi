// Package lexical implements deterministic Unicode-aware memory retrieval.
// It has no model, filesystem or network dependency.
package lexical

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func Normalize(s string) string { return strings.ToLower(norm.NFKC.String(s)) }

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) || r == 'ー'
}

func HasCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

// Terms includes whole CJK runs and overlapping bigrams. Script changes split
// Latin identifiers from CJK. Single Han characters remain meaningful; other
// one-character runs are omitted. ASCII keeps a three-character noise floor.
func Terms(s string) []string {
	var out []string
	for _, group := range termGroups(s) {
		out = append(out, group...)
	}
	return out
}

func termGroups(s string) [][]string {
	var groups [][]string
	var out []string
	var run []rune
	kind := 0
	flush := func() {
		if len(run) == 0 {
			return
		}
		if kind == 2 {
			if len(run) >= 2 {
				out = append(out, string(run))
				if len(run) > 2 {
					for i := 0; i+1 < len(run); i++ {
						out = append(out, string(run[i:i+2]))
					}
				}
				for _, r := range run {
					if unicode.Is(unicode.Han, r) {
						out = append(out, string(r))
					}
				}
			} else if unicode.Is(unicode.Han, run[0]) {
				out = append(out, string(run))
			}
		} else {
			word := string(run)
			minimum := 3
			if len(word) != utf8.RuneCountInString(word) {
				minimum = 2
			}
			if len(run) >= minimum {
				out = append(out, word)
			}
		}
		if len(out) > 0 {
			groups = append(groups, out)
			out = nil
		}
		run = nil
	}
	for _, r := range Normalize(s) {
		next := 0
		if isCJK(r) {
			next = 2
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			next = 1
		}
		if next == 0 {
			flush()
			kind = 0
			continue
		}
		if kind != next {
			flush()
		}
		kind = next
		run = append(run, r)
	}
	flush()
	return groups
}

// NonASCII returns additional terms without changing callers' legacy ASCII
// path/identifier rules. Deduplicate because a CJK bigram can recur in one run.
func NonASCII(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, term := range Terms(s) {
		if len(term) != utf8.RuneCountInString(term) && !seen[term] {
			seen[term] = true
			out = append(out, term)
		}
	}
	return out
}

// Scores uses BM25 (k1=1.2, b=.75), multiplied by squared query-word coverage.
// A CJK word's run and bigrams form one coverage group: a repeated partial word
// must not overwhelm another query word that actually identifies the answer.
// These are retrieval heuristics, not semantic or contradiction judgments.
func Scores(query string, documents []string) []float64 {
	scores := make([]float64, len(documents))
	queryTerms := map[string]bool{}
	for _, term := range Terms(query) {
		queryTerms[term] = true
	}
	if len(queryTerms) == 0 || len(documents) == 0 {
		return scores
	}
	counts := make([]map[string]int, len(documents))
	lengths := make([]int, len(documents))
	df := map[string]int{}
	total := 0
	for i, doc := range documents {
		terms := Terms(doc)
		lengths[i] = len(terms)
		total += len(terms)
		counts[i] = map[string]int{}
		for _, term := range terms {
			if queryTerms[term] {
				counts[i][term]++
			}
		}
		for term := range counts[i] {
			df[term]++
		}
	}
	if total == 0 {
		return scores
	}
	average := float64(total) / float64(len(documents))
	n := float64(len(documents))
	// Iterate the query slice in fixed order. Go map iteration would introduce
	// floating-point tie drift and unstable memory ordering across requests.
	var terms []string
	seen := map[string]bool{}
	for _, term := range Terms(query) {
		if !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	var groups [][]string
	groupSeen := map[string]bool{}
	for _, group := range termGroups(query) {
		key := strings.Join(group, "\x00")
		if !groupSeen[key] {
			groupSeen[key] = true
			groups = append(groups, group)
		}
	}
	for i := range documents {
		for _, term := range terms {
			tf := float64(counts[i][term])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[term])+.5)/(float64(df[term])+.5))
			scores[i] += idf * tf * 2.2 / (tf + 1.2*(.25+.75*float64(lengths[i])/average))
		}
		covered := 0
		for _, group := range groups {
			for _, term := range group {
				if counts[i][term] > 0 {
					covered++
					break
				}
			}
		}
		if len(groups) > 0 {
			coverage := float64(covered) / float64(len(groups))
			scores[i] *= coverage * coverage
		}
	}
	return scores
}
