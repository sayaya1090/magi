package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// snapSelCols floors the start and ceils the end to grapheme cell boundaries, so a
// selection edge can never bisect a wide character.
func TestSnapSelCols(t *testing.T) {
	line := "가나다라" // 4 Hangul, cells: 가[0,2) 나[2,4) 다[4,6) 라[6,8)
	cases := []struct{ c0, c1, want0, want1 int }{
		{0, 8, 0, 8},  // full line, already aligned
		{2, 6, 2, 6},  // aligned interior
		{3, 6, 2, 6},  // start bisects 나 → floor to 2
		{2, 5, 2, 6},  // end bisects 다 → ceil to 6
		{3, 5, 2, 6},  // both bisect
		{1, 2, 0, 2},  // single glyph, start mid → whole 가
		{0, 99, 0, 8}, // end clamped to line width
		{7, 8, 6, 8},  // start mid last glyph
	}
	for _, c := range cases {
		g0, g1 := snapSelCols(line, c.c0, c.c1)
		if g0 != c.want0 || g1 != c.want1 {
			t.Errorf("snapSelCols(%d,%d) = (%d,%d), want (%d,%d)", c.c0, c.c1, g0, g1, c.want0, c.want1)
		}
	}
	// ASCII is cell==grapheme: snapping is the identity.
	if g0, g1 := snapSelCols("abcdef", 2, 5); g0 != 2 || g1 != 5 {
		t.Errorf("ASCII snap changed the range: (%d,%d)", g0, g1)
	}
	// Mixed: "ab가cd" — cells a[0,1) b[1,2) 가[2,4) c[4,5) d[5,6).
	if g0, g1 := snapSelCols("ab가cd", 3, 5); g0 != 2 || g1 != 5 {
		t.Errorf("mixed snap = (%d,%d), want (2,5)", g0, g1)
	}
}

// A highlight whose endpoints land mid-glyph must still produce a line of the
// original width with consistent seams (the pre-fix cuts made the left segment
// drop a bisected glyph while the middle adopted it, so the edge jumped).
func TestHighlightSelectionWideChars(t *testing.T) {
	applyTheme(true)
	m := &Model{}
	m.contentLines = []string{"가나다라마바사"} // 14 cells
	m.contentPlain = []string{"가나다라마바사"}
	m.selActive = true
	m.selAL, m.selAC, m.selHL, m.selHC = 0, 3, 0, 9 // both endpoints bisect glyphs

	out := m.highlightSelection()
	if got := ansi.StringWidth(out); got != 14 {
		t.Errorf("highlighted line width = %d, want 14 (must not shrink)", got)
	}
	// The plain text must be unchanged — the highlight only recolors.
	if plain := ansi.Strip(out); plain != "가나다라마바사" {
		t.Errorf("highlight altered the text: %q", plain)
	}
	// The selected (recolored) span must cover whole glyphs: snapped to [2,10) = 나다라마.
	// Extract the styled middle by finding the selection style's reverse block.
	if !strings.Contains(out, "나다라마") {
		t.Errorf("selected span should contain the whole glyphs 나다라마, got %q", out)
	}
}

// The copied text matches the highlighted glyphs exactly.
func TestSelectedTextWideChars(t *testing.T) {
	m := &Model{}
	m.contentLines = []string{"가나다라마바사"}
	m.contentPlain = []string{"가나다라마바사"}
	m.selAL, m.selAC, m.selHL, m.selHC = 0, 3, 0, 9

	if got := m.selectedText(); got != "나다라마" {
		t.Errorf("selectedText = %q, want %q", got, "나다라마")
	}
}

// Apple Terminal reports mouse columns counting each character as one even though
// wide glyphs occupy two cells; charIndexToCell converts that character index to the
// glyph's starting cell so a drag in a Korean transcript lands where the pointer is.
func TestCharIndexToCell(t *testing.T) {
	line := "  가나다 abc 라마" // cells: sp sp 가(2-4) 나(4-6) 다(6-8) sp a b c sp 라 마
	cases := []struct{ idx, cell int }{
		{0, 0}, {1, 1},
		{2, 2},   // 가
		{3, 4},   // 나
		{4, 6},   // 다
		{5, 8},   // space
		{6, 9},   // a
		{9, 12},  // space
		{10, 13}, // 라
		{11, 15}, // 마
		{12, 17}, // end
		{15, 20}, // past end: 1:1 into the blank tail
	}
	for _, c := range cases {
		if got := charIndexToCell(line, c.idx); got != c.cell {
			t.Errorf("charIndexToCell(%d) = %d, want %d", c.idx, got, c.cell)
		}
	}
	// Pure ASCII: identity.
	if got := charIndexToCell("hello world", 7); got != 7 {
		t.Errorf("ascii identity broken: %d", got)
	}
}
