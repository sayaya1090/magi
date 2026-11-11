package tui

import (
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
)

// foldGlyph is the text bar in front of a progress line that carries a count — a compaction's
// summary parts. Counted only: with several parts the finished share fills it; with one part nobody
// knows how long the summary will run, so it shows a turning mark rather than an invented share.
// Empty when the note has no count, so the line reads exactly as it always did.
func foldGlyph(p *event.Progress) string {
	if p == nil {
		return ""
	}
	if p.Total <= 1 {
		if p.Total == 1 {
			return "⟳ 1/1 "
		}
		return "⟳ "
	}
	const cells = 8
	done := min(max(p.Done, 0), p.Total)
	n := done * cells / p.Total
	return strings.Repeat("▰", n) + strings.Repeat("▱", cells-n) + " " +
		strconv.Itoa(min(done+1, p.Total)) + "/" + strconv.Itoa(p.Total) + " "
}
