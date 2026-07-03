package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// ambiguousWide reports whether the host terminal draws East-Asian *ambiguous*
// width runes — the middle dot ·, em dash —, arrow →, star ★, and friends that
// pepper our output — as two cells instead of one. lipgloss/x/ansi measure them
// as one (narrow); when a terminal disagrees, every such rune on a line pushes
// the appended scrollbar one cell right, so the gutter looks ragged. The flag is
// decided once at startup by detectAmbiguousWidth (env override or a live CPR
// probe) and read only through cellWidth. Default false = narrow, which is both
// the common case and byte-identical to the previous behavior.
var ambiguousWide bool

// setAmbiguousWide records the probe/override result. Called once before the
// program starts; not safe for concurrent use with rendering (it isn't).
func setAmbiguousWide(w bool) { ambiguousWide = w }

// narrowCond and wideCond exist only to classify a rune as ambiguous: a rune is
// ambiguous exactly when it measures 1 narrow but 2 wide. StrictEmojiNeutral
// keeps emoji classification stable so we don't accidentally treat emoji as
// ambiguous.
var (
	narrowCond = &runewidth.Condition{EastAsianWidth: false, StrictEmojiNeutral: true}
	wideCond   = &runewidth.Condition{EastAsianWidth: true, StrictEmojiNeutral: true}
)

// cellWidth returns the display width of s in terminal cells. It is a drop-in
// replacement for ansi.StringWidth: when the terminal treats ambiguous runes as
// narrow (the default) the two are identical, so swapping call sites is a no-op.
// When the terminal is known to draw them wide, cellWidth adds one cell per
// ambiguous rune — the correction that keeps the scrollbar and other fixed
// columns aligned. Emoji/grapheme handling is left to ansi.StringWidth so wrap
// widths stay consistent with lipgloss.
func cellWidth(s string) int {
	w := ansi.StringWidth(s)
	if ambiguousWide {
		w += ambiguousExtra(ansi.Strip(s))
	}
	return w
}

// ambiguousExtra counts ambiguous-width runes in an already-ANSI-stripped
// string — the extra cells they consume when the terminal renders them wide.
func ambiguousExtra(plain string) int {
	n := 0
	for _, r := range plain {
		if narrowCond.RuneWidth(r) == 1 && wideCond.RuneWidth(r) == 2 {
			n++
		}
	}
	return n
}

// padOrTruncate lays s out to exactly w display cells (per cellWidth): right-pad
// with spaces when short, ANSI-aware-truncate when long. Truncation re-measures
// with cellWidth so an ambiguous-heavy line is cut to the real terminal width,
// not lipgloss's; any cell lost to a clipped wide rune is padded back with a
// space so the result is always exactly w wide.
func padOrTruncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	cw := cellWidth(s)
	if cw == w {
		return s
	}
	if cw < w {
		return s + strings.Repeat(" ", w-cw)
	}
	// Truncate down until it fits (target reaches 0 → empty), then pad back any
	// cell lost to a clipped wide/ambiguous rune so the result is exactly w.
	// ansi.Truncate keeps escape bytes but adds no trailing reset, so cap an
	// unterminated color run before the padding/gutter to stop it bleeding right.
	t := ""
	for target := w; target >= 0; target-- {
		t = ansi.Truncate(s, target, "")
		if cellWidth(t) <= w {
			break
		}
	}
	if strings.Contains(t, "\x1b") {
		t += "\x1b[0m"
	}
	if d := w - cellWidth(t); d > 0 {
		t += strings.Repeat(" ", d)
	}
	return t
}

// composeBox renders `content` into a full height×width block. Each line is
// forced to exactly `width` cells via padOrTruncate — our terminal-aware
// measure, not lipgloss's — and missing rows become blanks, so a short
// transcript still fills the viewport and the panes/input below sit at the
// bottom of the screen. (This used to also weld a 1-col scrollbar gutter; the
// drawn scrollbar is gone — scroll position lives in the header chip — which
// retires the ambiguous-width misalignment class it kept regressing on.)
func composeBox(content string, width, height int) string {
	if height <= 0 {
		return ""
	}
	contentLines := strings.Split(content, "\n")
	var b strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := ""
		if i < len(contentLines) {
			line = contentLines[i]
		}
		b.WriteString(padOrTruncate(line, width))
	}
	return b.String()
}
