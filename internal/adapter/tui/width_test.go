package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// withAmbiguousWide runs fn with the flag set, restoring it after — the flag is
// process-global, so tests must not leak it.
func withAmbiguousWide(t *testing.T, v bool, fn func()) {
	t.Helper()
	prev := ambiguousWide
	setAmbiguousWide(v)
	defer setAmbiguousWide(prev)
	fn()
}

// TestCellWidthNarrowMatchesAnsi pins the invariant that makes swapping call
// sites safe: with the default (narrow) flag, cellWidth == ansi.StringWidth for
// everything, including ambiguous runes and emoji.
func TestCellWidthNarrowMatchesAnsi(t *testing.T) {
	withAmbiguousWide(t, false, func() {
		for _, s := range []string{
			"plain ascii", "", "· — → ★ ⚙", "emoji ✅⏳⚠️🏁", "한글 CJK 字",
			"\x1b[31mred\x1b[0m", "mix a·b→c ✅ 字",
		} {
			if got, want := cellWidth(s), ansi.StringWidth(s); got != want {
				t.Errorf("cellWidth(%q)=%d, ansi=%d (must match when narrow)", s, got, want)
			}
		}
	})
}

// TestCellWidthWideAddsAmbiguous checks that the wide flag adds exactly one cell
// per ambiguous rune and leaves ASCII/emoji/CJK width untouched.
func TestCellWidthWideAddsAmbiguous(t *testing.T) {
	cases := []struct {
		s             string
		extraWhenWide int
	}{
		{"ascii only", 0},
		{"·", 1},     // middle dot: ambiguous
		{"· — →", 3}, // three ambiguous (spaces are not)
		{"★ x ★", 2}, // two stars
		{"字", 0},     // CJK wide already 2 in both modes
		{"✅", 0},     // emoji already 2, not ambiguous
	}
	for _, c := range cases {
		var narrow, wide int
		withAmbiguousWide(t, false, func() { narrow = cellWidth(c.s) })
		withAmbiguousWide(t, true, func() { wide = cellWidth(c.s) })
		if wide-narrow != c.extraWhenWide {
			t.Errorf("%q: wide-narrow=%d, want %d (narrow=%d wide=%d)", c.s, wide-narrow, c.extraWhenWide, narrow, wide)
		}
	}
}

// withDecorWide runs fn with decorWide set to mark every decorGlyph wide (or
// unset), restoring the process-global after.
func withDecorWide(t *testing.T, wide bool, fn func()) {
	t.Helper()
	prev := decorWide
	if wide {
		m := make(map[rune]bool, len(decorGlyphs))
		for _, r := range decorGlyphs {
			m[r] = true
		}
		setDecorWide(m)
	} else {
		setDecorWide(nil)
	}
	defer setDecorWide(prev)
	fn()
}

// TestCellWidthDecorWide checks that when the terminal draws the decorative
// glyphs wide, cellWidth adds exactly one cell per such glyph — and that these
// glyphs are NOT already caught by the ambiguous correction (so the decor probe
// is genuinely needed), nor double-counted.
func TestCellWidthDecorWide(t *testing.T) {
	// Ambiguous correction alone must NOT move these glyphs: they measure 1 in both
	// runewidth modes, so ambiguousExtra can't see them — this is the O1 gap.
	for _, g := range []string{"‹", "›", "✦", "✻", "⚖", "⇅"} {
		var narrow, ambWide int
		withDecorWide(t, false, func() {
			withAmbiguousWide(t, false, func() { narrow = cellWidth(g) })
			withAmbiguousWide(t, true, func() { ambWide = cellWidth(g) })
		})
		if narrow != 1 || ambWide != 1 {
			t.Errorf("decor glyph %q: narrow=%d ambiguousWide=%d, want 1/1 (ambiguous correction must not touch it)", g, narrow, ambWide)
		}
	}
	// With the decor override on, each drawn-wide glyph costs one extra cell.
	cases := []struct {
		s     string
		extra int
	}{
		{"ascii", 0},
		{"‹ back", 1},   // one guillemet
		{"✦ magi ⚖", 2}, // brand + council
		{"‹›✦✻⚖⇅", 6},   // all six
		{"·—→", 0},      // ambiguous set is NOT in decorWide (handled separately)
	}
	for _, c := range cases {
		var off, on int
		withDecorWide(t, false, func() { off = cellWidth(c.s) })
		withDecorWide(t, true, func() { on = cellWidth(c.s) })
		if on-off != c.extra {
			t.Errorf("%q: on-off=%d, want %d (off=%d on=%d)", c.s, on-off, c.extra, off, on)
		}
	}
}

// TestPadOrTruncateExactWidth: the result is always exactly w cells (per
// cellWidth), whether the input is short (padded), exact, or long (truncated),
// in both width modes and with ANSI styling present.
func TestPadOrTruncateExactWidth(t *testing.T) {
	inputs := []string{"", "hi", "exactly-ten", "· — → ★ longish ambiguous line ★ → — ·", "\x1b[31mred text\x1b[0m padding"}
	for _, wide := range []bool{false, true} {
		withAmbiguousWide(t, wide, func() {
			for _, s := range inputs {
				for _, w := range []int{1, 5, 12, 40} {
					got := padOrTruncate(s, w)
					if cw := cellWidth(got); cw != w {
						t.Errorf("wide=%v padOrTruncate(%q,%d) width=%d, want %d (got %q)", wide, s, w, cw, w, got)
					}
				}
			}
		})
	}
}

// TestComposeBoxAlignment: every rendered row must be exactly width cells,
// even for rows dense with ambiguous characters and even when the terminal
// treats them as wide — the invariant that keeps the panel column straight.
func TestComposeBoxAlignment(t *testing.T) {
	content := strings.Join([]string{
		"short",
		"a line with · — → ★ special chars",
		"", // blank row
		"emoji ✅ ⏳ ⚠️ and ambiguous · · ·",
	}, "\n")
	const width, height = 30, 6
	for _, wide := range []bool{false, true} {
		withAmbiguousWide(t, wide, func() {
			out := composeBox(content, width, height)
			rows := strings.Split(out, "\n")
			if len(rows) != height {
				t.Fatalf("wide=%v got %d rows, want %d", wide, len(rows), height)
			}
			for i, r := range rows {
				if cw := cellWidth(r); cw != width {
					t.Errorf("wide=%v row %d width=%d, want %d (%q)", wide, i, cw, width, r)
				}
			}
		})
	}
}

// TestComposeBoxZeroHeight: defensive — no rows, no panic.
func TestComposeBoxZeroHeight(t *testing.T) {
	if got := composeBox("anything", 10, 0); got != "" {
		t.Errorf("height 0 should render empty, got %q", got)
	}
}
