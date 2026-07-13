package builtin

import (
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Hashline gives every read line a short content fingerprint the model can quote
// back when it edits. A read line renders as "N#hh|content": N is the 1-based
// line number, hh is a 2-char hash of the line's content, "|" separates the
// metadata from the text. An edit may then anchor to "N#hh" (the `at` field); the
// tool recomputes the hash from the CURRENT file and rejects the edit if it no
// longer matches — a deterministic guard against editing a line the model saw in
// a stale read or misremembered. It attacks the churn-to-wall failure where an
// unverified mutation lands on the wrong line and regresses passing state.
//
// The hash normalizes away trailing whitespace and line-ending drift (the same
// slack the edit matcher already tolerates) so it is stable across CRLF/LF and
// trailing-space differences, but leading indentation IS significant — that is
// load-bearing in most languages and the edit matcher requires it to match.

// hashAlphabet maps a nibble (0–15) to a distinct consonant, so a hash is two
// visually unambiguous letters: no vowels (can't spell an accidental word) and
// none of the 0/O, 1/l/I shapes that read ambiguously in a terminal.
const hashAlphabet = "BCDFGHJKLMNPRSTV"

// lineHash returns the 2-char content fingerprint of a single line. Trailing
// whitespace and any CR/LF are stripped before hashing so the same logical line
// hashes identically regardless of EOL style or trailing-space churn.
func lineHash(content string) string {
	norm := strings.TrimRight(content, " \t\r\n")
	h := fnv.New32a()
	_, _ = io.WriteString(h, norm)
	sum := h.Sum32()
	// Fold the 32-bit sum into one byte so the 2-char code depends on the whole hash.
	b := byte(sum ^ (sum >> 8) ^ (sum >> 16) ^ (sum >> 24))
	return string([]byte{hashAlphabet[b>>4], hashAlphabet[b&0x0f]})
}

// formatHashLine renders one numbered line as "N#hh|content".
func formatHashLine(n int, content string) string {
	return fmt.Sprintf("%d#%s|%s", n, lineHash(content), content)
}

// parseLineRef parses an edit anchor of the form "N" or "N#hh" into its 1-based
// line number and (optional) expected hash. A bare "N" anchors by position only,
// trading the content check for a plain line-number guard. Leading/trailing space
// is tolerated so a model that pastes "42#hh " still parses.
func parseLineRef(ref string) (line int, hash string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, "", false
	}
	num, rest, hasHash := strings.Cut(ref, "#")
	n, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil || n < 1 {
		return 0, "", false
	}
	if !hasHash {
		return n, "", true
	}
	return n, strings.TrimSpace(rest), true
}

// fileLines splits content into lines, each retaining its terminator, so the
// count equals the number of lines the read tool numbered. "a\nb\n" → 2 lines,
// "a\nb" → 2 lines, "" → 1 (empty) line.
func fileLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	parts := strings.SplitAfter(content, "\n")
	if parts[len(parts)-1] == "" { // trailing "" after a final newline
		parts = parts[:len(parts)-1]
	}
	return parts
}

// checkAnchor validates that ref points at an existing line whose current hash
// matches the one the model quoted. It returns the 1-based line index on success
// or an error that names the current hash so the model can re-anchor without a
// blind retry. A ref without a hash only bounds-checks the line number.
func checkAnchor(content, ref string) (int, error) {
	n, want, ok := parseLineRef(ref)
	if !ok {
		return 0, fmt.Errorf("invalid line ref %q (expected \"N\" or \"N#hh\" from a read)", ref)
	}
	lines := fileLines(content)
	if n > len(lines) {
		return 0, fmt.Errorf("line %d is past end of file (%d lines) — re-read to get current line refs", n, len(lines))
	}
	if want == "" {
		return n, nil
	}
	cur := strings.TrimRight(lines[n-1], "\r\n")
	if got := lineHash(cur); got != want {
		return 0, fmt.Errorf("stale line ref: line %d now hashes #%s not #%s (current content: %q) — re-read the file to get fresh refs", n, got, want, clipRef(cur, 80))
	}
	return n, nil
}

// replaceLineRange splices lines [from,to] (1-based, inclusive) with repl,
// preserving the terminator style of the replaced region. An empty repl deletes
// the range. from/to are assumed valid (checkAnchor guards them).
func replaceLineRange(content string, from, to int, repl string) string {
	lines := fileLines(content)
	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}
	region := strings.Join(lines[from-1:to], "")
	crlf := strings.HasSuffix(region, "\r\n")
	term := ""
	if crlf {
		term = "\r\n"
	} else if strings.HasSuffix(region, "\n") {
		term = "\n"
	}
	if repl == "" {
		lines[from-1] = "" // drop content and terminator
	} else {
		r := repl
		if crlf {
			r = toEOL(repl, true)
		}
		if term != "" && !strings.HasSuffix(r, term) {
			r += term
		}
		lines[from-1] = r
	}
	// Collapse the replaced span into the first slot.
	out := append([]string{}, lines[:from]...)
	out = append(out, lines[to:]...)
	return strings.Join(out, "")
}

// hashPrefixRe matches the "N#hh|" metadata the read tool prepends to each line.
var hashPrefixRe = regexp.MustCompile(`^\d+#[` + hashAlphabet + `]{2}\|`)

// stripHashPrefixes removes a leading "N#hh|" from every line of s, but only when
// every non-blank line carries one — so a model that pasted read output verbatim
// into `old` still matches, while ordinary source (which won't uniformly carry
// the marker) is left untouched. Returns the cleaned text and whether it changed.
func stripHashPrefixes(s string) (string, bool) {
	lines := strings.Split(s, "\n")
	any := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !hashPrefixRe.MatchString(l) {
			return s, false // not uniformly prefixed → leave as-is
		}
		any = true
	}
	if !any {
		return s, false
	}
	for i, l := range lines {
		lines[i] = hashPrefixRe.ReplaceAllString(l, "")
	}
	return strings.Join(lines, "\n"), true
}

// clipRef trims s to n runes with an ellipsis, for one-line error context.
func clipRef(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
