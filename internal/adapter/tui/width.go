package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
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

// decorWide maps each decorative glyph the TUI draws to whether THIS terminal
// renders it two cells wide. It is nil until detectDecorWidths runs. These glyphs
// (decorGlyphs) all measure 1 in BOTH go-runewidth modes (narrow==wide==1), so
// ambiguousExtra can never correct them — yet some terminals (notably Windows
// Terminal) draw dingbats like ✦ ✻ ⚖ two cells wide, leaving every line that
// carries one measured short and mis-padded. Because their real width can't be
// derived from Unicode tables, it is measured per-glyph at startup. nil or an
// all-false map = no correction = byte-identical to the previous behavior.
var decorWide map[rune]bool

// decorGlyphs is the set of "measures-1-everywhere but may render wide" glyphs the
// TUI actually draws: ‹ › (guillemet breadcrumbs), ✦ (brand), ✻ (thought),
// ⚖ (council), ⇅ (scroll meter). Every OTHER decorative glyph we draw
// (· … → ← ↑ ↓ ★ ● ◆ ◈ ⛐ │ ─) is East-Asian *ambiguous* and already handled by
// ambiguousExtra, so it is deliberately excluded here to avoid double-counting.
var decorGlyphs = []rune{'‹', '›', '✦', '✻', '⚖', '⇅'}

// setDecorWide records the per-glyph probe/override result.
func setDecorWide(m map[rune]bool) { decorWide = m }

// emojiNarrow reports whether THIS terminal draws emoji one cell wide instead of
// the two cells ansi.StringWidth/lipgloss assume. Many terminals (and fonts) draw
// 🚀 and friends in a single cell; when they do, every emoji on a line leaves the
// measured width one cell too long, so a fixed right column (the status-panel
// border) is pushed out of alignment on exactly the rows that carry one. Decided
// once at startup by detectEmojiWidth (env override or a live CPR probe) and read
// only through cellWidth. Default false = wide, byte-identical to prior behavior.
var emojiNarrow bool

// graphemeWidthCache memoizes, per grapheme CLUSTER, the cellWidth *correction* an
// emoji-narrow terminal needs: -1 for an unstable-wide emoji (ansi says 2, the
// terminal draws 1) or 0 otherwise. It is the "문자마다 계산된 실측폭을 메모리에 캐시"
// store — each glyph is classified once, then reused. Keyed by the FULL cluster
// string, not its first rune: a keycap like "1️⃣" (base '1' + VS16 + U+20E3) and the
// bare "1" share a first rune but have different widths, so a base-rune key would
// let one poison the other. Populated lazily by emojiExtra during rendering (no
// probe: pure computation), reset by setEmojiNarrow so a re-decided flag can't leave
// stale entries. Single-goroutine render path — no locking.
var graphemeWidthCache = map[string]int{}

// setEmojiNarrow records the probe/override result and clears the per-grapheme cache
// (its classifications were computed under the previous flag). Called once before
// the program starts; not safe for concurrent use with rendering (it isn't).
func setEmojiNarrow(w bool) {
	emojiNarrow = w
	graphemeWidthCache = map[string]int{}
}

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
// columns aligned. When the terminal draws emoji narrow (emojiNarrow), it also
// subtracts one cell per unstable-wide emoji grapheme (see emojiExtra), keyed off
// a per-cluster cache; otherwise emoji/grapheme handling is left to ansi.StringWidth
// so wrap widths stay consistent with lipgloss.
func cellWidth(s string) int {
	w := ansi.StringWidth(s)
	if !ambiguousWide && decorWide == nil && !emojiNarrow {
		return w
	}
	plain := ansi.Strip(s)
	if ambiguousWide {
		w += ambiguousExtra(plain)
	}
	if decorWide != nil {
		w += decorExtra(plain)
	}
	if emojiNarrow {
		w += emojiExtra(plain)
	}
	return w
}

// emojiExtra returns the (negative) cell correction for an already-ANSI-stripped
// string on an emoji-narrow terminal: -1 per grapheme cluster that ansi.StringWidth
// counts as two cells but the terminal actually draws in one (isUnstableWide). It
// walks grapheme clusters (rivo/uniseg) so a multi-rune emoji is one unit, and
// memoizes each classification by the full cluster string in graphemeWidthCache.
func emojiExtra(plain string) int {
	n := 0
	g := uniseg.NewGraphemes(plain)
	for g.Next() {
		cluster := g.Str()
		if cluster == "" {
			continue
		}
		if d, ok := graphemeWidthCache[cluster]; ok {
			n += d
			continue
		}
		d := 0
		if isUnstableWide(cluster) {
			d = -1
		}
		graphemeWidthCache[cluster] = d
		n += d
	}
	return n
}

// isUnstableWide reports whether a grapheme cluster is an emoji whose on-screen
// width is terminal-dependent — ansi.StringWidth measures it as two cells, but many
// terminals/fonts draw it in one. It uses an ALLOWLIST of emoji signals rather than
// a denylist of CJK blocks: only true emoji are shrunk, so every stable-wide script
// (CJK ideographs, kana, Hangul incl. compatibility jamo, fullwidth forms, enclosed
// CJK) is safe by construction — critical since over-shrinking Korean text is a
// worse failure than leaving a rare emoji un-shrunk. The width==2 guard already
// excludes ambiguous/decor glyphs (★ · → etc., which measure 1). Emoji signals: a
// variation selector (VS16) or keycap combiner anywhere in the cluster, or a first
// rune in the pictographic blocks.
func isUnstableWide(cluster string) bool {
	if ansi.StringWidth(cluster) != 2 {
		return false
	}
	for _, r := range cluster {
		switch {
		case r == 0xFE0F: // VS16: emoji-presentation selector (e.g. ▶️, 1️⃣)
			return true
		case r == 0x20E3: // combining enclosing keycap (0️⃣–9️⃣, #️⃣, *️⃣)
			return true
		case r >= 0x1F000 && r <= 0x1FAFF: // emoji & pictographs (all blocks, incl. flags 1F1E6+)
			return true
		case r >= 0x2600 && r <= 0x27BF: // Misc Symbols + Dingbats (the width-2 ones, e.g. ✅)
			return true
		case r >= 0x2B00 && r <= 0x2BFF: // Misc Symbols and Arrows (⭐ ⬛ ⬆ …)
			return true
		}
	}
	return false
}

// decorExtra counts the extra cells consumed by decorative glyphs this terminal
// draws wide (per detectDecorWidths), on an already-ANSI-stripped string. These
// glyphs measure 1 via ansi.StringWidth, so each wide-drawn one costs one more.
// decorGlyphs never overlaps the ambiguous set, so this is additive with
// ambiguousExtra, never double-counting a rune.
func decorExtra(plain string) int {
	n := 0
	for _, r := range plain {
		if decorWide[r] {
			n++
		}
	}
	return n
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
