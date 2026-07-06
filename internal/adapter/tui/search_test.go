package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
)

func newSearchModel() *Model {
	applyTheme(true)
	m := &Model{}
	m.vp = viewport.New()
	m.vp.SetWidth(40)
	m.vp.SetHeight(4)
	m.contentPlain = []string{
		"alpha line", "nothing here", "ERROR: build failed", "middle",
		"another error: tests", "tail line", "x", "y", "z", "w",
	}
	m.vp.SetContent(strings.Join(m.contentPlain, "\n"))
	m.contentLines = append([]string(nil), m.contentPlain...)
	m.ready = false // refresh() is a no-op in tests (no full layout)
	return m
}

// updateSearch finds case-insensitive hits and navigation wraps both ways.
func TestSearchHitsAndStep(t *testing.T) {
	m := newSearchModel()
	m.searching = true
	m.searchQuery = "error"
	m.updateSearch()
	if len(m.searchHits) != 2 || m.searchHits[0] != 2 || m.searchHits[1] != 4 {
		t.Fatalf("hits = %v, want [2 4]", m.searchHits)
	}
	if m.searchCur != 0 {
		t.Fatalf("first hit at/after offset 0 should be selected, got %d", m.searchCur)
	}
	m.searchStep(1)
	if m.searchHits[m.searchCur] != 4 {
		t.Fatalf("next should land on line 4, got %d", m.searchHits[m.searchCur])
	}
	m.searchStep(1) // wrap
	if m.searchHits[m.searchCur] != 2 {
		t.Fatalf("next past the end should wrap to line 2, got %d", m.searchHits[m.searchCur])
	}
	m.searchStep(-1) // wrap backwards
	if m.searchHits[m.searchCur] != 4 {
		t.Fatalf("prev past the start should wrap to line 4, got %d", m.searchHits[m.searchCur])
	}
}

// The overlay highlights every match without changing the row count, and the
// bar shows the position; an empty query clears the hit list.
func TestSearchHighlightAndView(t *testing.T) {
	m := newSearchModel()
	m.searching = true
	m.searchQuery = "error"
	m.updateSearch()
	out := m.highlightSearch(strings.Join(m.contentLines, "\n"))
	if n := len(strings.Split(out, "\n")); n != len(m.contentPlain) {
		t.Fatalf("highlight changed the row count: %d != %d", n, len(m.contentPlain))
	}
	if stripANSI(out) != strings.Join(m.contentPlain, "\n") {
		t.Fatalf("highlight must not alter the text itself")
	}
	if v := stripANSI(m.searchView()); !strings.Contains(v, "1/2") && !strings.Contains(v, "2/2") {
		t.Fatalf("search bar should show position, got %q", v)
	}
	m.searchQuery = ""
	m.updateSearch()
	if len(m.searchHits) != 0 {
		t.Fatalf("empty query should clear hits, got %v", m.searchHits)
	}
}

// sgrRE matches an SGR escape; highlightedSpans returns the visible text that
// highlightSearch wrapped in a non-reset style. In these tests contentLines carry
// no pre-existing ANSI, so every styled span is a highlight.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func highlightedSpans(s string) []string {
	var runs []string
	var cur strings.Builder
	in, i := false, 0
	for i < len(s) {
		if loc := sgrRE.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
			reset := s[i:i+loc[1]] == "\x1b[0m" || s[i:i+loc[1]] == "\x1b[m"
			if in && reset {
				if cur.Len() > 0 {
					runs = append(runs, cur.String())
					cur.Reset()
				}
				in = false
			} else if !reset {
				in = true
			}
			i += loc[1]
			continue
		}
		if in {
			cur.WriteByte(s[i])
		}
		i++
	}
	if cur.Len() > 0 {
		runs = append(runs, cur.String())
	}
	return runs
}

// TestSearchHighlightAmbiguousWide guards O13, an O8-class mismatch: highlightSearch
// computed its cut columns with cellWidth but cuts with ansi.Cut (StringWidth), so
// on a terminal that draws ambiguous runes wide the highlight slid right by one
// column per ambiguous rune in the prefix — tinting the wrong characters.
func TestSearchHighlightAmbiguousWide(t *testing.T) {
	applyTheme(true)
	setAmbiguousWide(true)
	defer setAmbiguousWide(false)

	m := &Model{}
	m.vp = viewport.New()
	m.vp.SetWidth(40)
	m.vp.SetHeight(4)
	m.contentPlain = []string{"★★★error here"} // 3 ambiguous-width runes before the match
	m.contentLines = append([]string(nil), m.contentPlain...)
	m.vp.SetContent(strings.Join(m.contentPlain, "\n"))
	m.searching = true
	m.searchQuery = "error"
	m.updateSearch()

	spans := highlightedSpans(m.highlightSearch(strings.Join(m.contentLines, "\n")))
	if len(spans) != 1 || spans[0] != "error" {
		t.Errorf("highlight tinted %q, want exactly [\"error\"]", spans)
	}
}

// relAge: compact within a week, empty (→ absolute fallback) beyond or for zero.
func TestRelAge(t *testing.T) {
	if got := relAge(time.Now().Add(-30 * time.Second)); got != "30s ago" {
		t.Errorf("30s: %q", got)
	}
	if got := relAge(time.Now().Add(-3 * time.Hour)); got != "3h ago" {
		t.Errorf("3h: %q", got)
	}
	if got := relAge(time.Now().Add(-9 * 24 * time.Hour)); got != "" {
		t.Errorf("old sessions should fall back to absolute, got %q", got)
	}
	if got := relAge(time.Time{}); got != "" {
		t.Errorf("zero time should yield empty, got %q", got)
	}
}
