package builtin

import (
	"fmt"
	"strconv"
	"strings"
)

// Hashline numbers every read line so the model can quote it back and anchor an
// edit to it. A read line renders as "N\tcontent" — the 1-based line number, a
// TAB, then the text: the universally recognized `cat -n` gutter, which models
// reliably read as metadata and do NOT mistake for file content. (An earlier
// "N#hh|content" form carried a per-line content hash for a stale-anchor guard,
// but its `#hh|` gutter looked like a pipe-delimited data column and misled the
// model into parsing a phantom prefix on structured text — CSV/TSV/fixed-width —
// costing whole tasks. The tab gutter removes that failure mode; the edit anchor
// is now a plain line number, guarded by a bounds check, not a content hash.)

// formatNumberedLine renders one line as "N\tcontent" (cat -n style).
func formatNumberedLine(n int, content string) string {
	return fmt.Sprintf("%d\t%s", n, content)
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

// clipRef trims s to n runes with an ellipsis, for one-line error context.
func clipRef(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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

// checkAnchor validates that ref points at an existing line, returning the 1-based
// line index. The anchor is a plain line number "N" from a read; it is guarded by a
// bounds check (past-end refs are rejected so a stale over-long read can't write off
// the end). A trailing "#…" from an old-style ref is tolerated and ignored.
func checkAnchor(content, ref string) (int, error) {
	n, _, ok := parseLineRef(ref)
	if !ok {
		return 0, fmt.Errorf("invalid line ref %q (expected a line number \"N\" from a read)", ref)
	}
	lines := fileLines(content)
	if n > len(lines) {
		return 0, fmt.Errorf("line %d is past end of file (%d lines) — re-read to get current line numbers", n, len(lines))
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
