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

const (
	DefaultK1 = 1.2
	DefaultB  = 0.45 // Lowered from 0.75 for concise agent memory & rules
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

var koreanSuffixes = []string{
	"에서", "으로", "하고", "이다", "했다",
	"은", "는", "이", "가", "을", "를", "에", "로", "의", "와", "과",
}

func stripKoreanSuffix(s string) string {
	runes := []rune(s)
	if len(runes) <= 1 {
		return ""
	}
	for _, suf := range koreanSuffixes {
		sufRunes := []rune(suf)
		if len(runes) > len(sufRunes) && strings.HasSuffix(s, suf) {
			stem := string(runes[:len(runes)-len(sufRunes)])
			if len([]rune(stem)) >= 1 {
				return stem
			}
		}
	}
	return ""
}

var techNoStemWords = map[string]bool{
	"docker": true, "server": true, "buffer": true, "string": true,
	"padding": true, "driver": true, "number": true, "filter": true,
	"router": true, "cluster": true, "caller": true, "helper": true,
}

func lightStem(w string) string {
	if len(w) <= 4 || techNoStemWords[w] {
		return ""
	}
	if strings.HasSuffix(w, "ing") && len(w) > 5 {
		return strings.TrimSuffix(w, "ing")
	}
	if strings.HasSuffix(w, "ed") && len(w) > 4 {
		return strings.TrimSuffix(w, "ed")
	}
	if strings.HasSuffix(w, "es") && len(w) > 4 {
		return strings.TrimSuffix(w, "es")
	}
	if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 4 {
		return strings.TrimSuffix(w, "s")
	}
	return ""
}

// splitCamelCase splits CamelCase words into sub-words (e.g. SearchManagedExperience -> [Search, Managed, Experience]).
func splitCamelCase(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	n := len(runes)
	for i := 0; i < n; i++ {
		r := runes[i]
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextIsLower := (i+1 < n) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextIsLower) {
				if len(cur) > 0 {
					words = append(words, string(cur))
					cur = nil
				}
			}
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

// isCodeDelimiter returns true for punctuation separating identifiers or paths.
func isCodeDelimiter(r rune) bool {
	switch r {
	case '/', '\\', '.', '-', '_', '@', ':', '=', '+', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'', ',', ';', '?', '!', '&', '|', '~', '^', '%', '$', '#', '*':
		return true
	}
	return unicode.IsSpace(r)
}

func cjkRunTerms(run []rune) []string {
	if len(run) == 0 {
		return nil
	}
	var out []string
	word := string(run)
	if len(run) >= 2 {
		out = append(out, word)
		for i := 0; i+1 < len(run); i++ {
			out = append(out, string(run[i:i+2]))
		}
		// First unigram for Hangul/CJK eojeol preservation
		out = append(out, string(run[0]))
		for _, r := range run {
			if unicode.Is(unicode.Han, r) {
				out = append(out, string(r))
			}
		}
		// Suffix stripping for Korean
		if stem := stripKoreanSuffix(word); stem != "" {
			stemRunes := []rune(stem)
			out = append(out, stem)
			if len(stemRunes) >= 2 {
				for i := 0; i+1 < len(stemRunes); i++ {
					out = append(out, string(stemRunes[i:i+2]))
				}
			}
		}
	} else if unicode.Is(unicode.Han, run[0]) || unicode.In(run[0], unicode.Hangul) {
		// Single Han or single Hangul character preserved
		out = append(out, word)
	}
	return out
}

// Terms includes whole CJK runs, overlapping bigrams, 1-character CJK unigrams,
// CamelCase sub-tokens, path sub-tokens, and light English stems.
func Terms(s string) []string {
	var out []string
	for _, group := range termGroups(s) {
		out = append(out, group...)
	}
	return out
}

func termGroups(s string) [][]string {
	var groups [][]string
	nfkc := norm.NFKC.String(s)

	// Step 1: Split by whitespace & punctuation into raw chunks
	rawWords := strings.FieldsFunc(nfkc, isCodeDelimiter)
	for _, raw := range rawWords {
		var group []string
		seenInGroup := map[string]bool{}
		addTerm := func(t string) {
			t = strings.TrimSpace(t)
			if t != "" && !seenInGroup[t] {
				seenInGroup[t] = true
				group = append(group, t)
			}
		}

		// Sub-tokenization for code identifiers
		camelParts := splitCamelCase(raw)
		normRaw := Normalize(raw)
		if len(camelParts) > 1 && utf8.RuneCountInString(normRaw) >= 3 {
			addTerm(normRaw)
		}

		parts := camelParts
		if len(parts) == 0 {
			parts = []string{raw}
		}

		for _, part := range parts {
			normPart := Normalize(part)
			var run []rune
			kind := 0 // 1: alphanumeric, 2: CJK

			flush := func() {
				if len(run) == 0 {
					return
				}
				if kind == 2 {
					for _, ct := range cjkRunTerms(run) {
						addTerm(ct)
					}
				} else {
					w := string(run)
					runes := []rune(w)
					// Noise floor: single ASCII letters or single digits are omitted.
					// 2-character ASCII words (go, os, ci, cd, ip, db, ui) are preserved.
					if len(runes) >= 2 {
						addTerm(w)
						if stem := lightStem(w); stem != "" {
							addTerm(stem)
						}
					}
				}
				run = nil
			}

			for _, r := range normPart {
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
		}

		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
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

// Scores uses BM25 with k1=DefaultK1, b=DefaultB, multiplied by squared query-word coverage.
func Scores(query string, documents []string) []float64 {
	return ScoresWithParams(query, documents, DefaultK1, DefaultB)
}

// ScoresWithParams calculates BM25 scores with custom k1 and b parameters.
func ScoresWithParams(query string, documents []string, k1, b float64) []float64 {
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
			scores[i] += idf * tf * (k1 + 1.0) / (tf + k1*((1.0-b)+b*float64(lengths[i])/average))
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

// Field represents a weighted text component of a document for BM25F scoring.
type Field struct {
	Text   string
	Weight float64
}

// DocumentFields encapsulates the structured fields of a document.
type DocumentFields struct {
	Fields []Field
}

// FieldScores calculates field-weighted BM25 scores for documents across multiple fields.
func FieldScores(query string, docs []DocumentFields) []float64 {
	if len(docs) == 0 {
		return nil
	}
	totalScores := make([]float64, len(docs))
	maxFields := 0
	for _, d := range docs {
		if len(d.Fields) > maxFields {
			maxFields = len(d.Fields)
		}
	}
	for f := 0; f < maxFields; f++ {
		texts := make([]string, len(docs))
		weight := 1.0
		for i, d := range docs {
			if f < len(d.Fields) {
				texts[i] = d.Fields[f].Text
				if d.Fields[f].Weight > 0 {
					weight = d.Fields[f].Weight
				}
			}
		}
		fScores := Scores(query, texts)
		for i, sc := range fScores {
			totalScores[i] += weight * sc
		}
	}
	return totalScores
}
