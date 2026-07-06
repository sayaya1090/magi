package tui

import (
	"os"
	"strconv"
	"strings"
)

// probeGlyph is the test character the startup probe measures. It must be a rune
// that (a) go-runewidth classifies as East-Asian *ambiguous* (so its measured
// width actually predicts the ambiguousExtra correction) and (b) is one of the
// structural, alignment-critical runes the TUI itself draws — the panel border.
// We deliberately use │ (U+2502) rather than a decorative ambiguous glyph like ★
// (U+2605): Windows Terminal draws ★ two cells wide but draws │, █, ·, →, — one
// cell each, so probing ★ set ambiguousWide=true and then over-measured every
// structural line by one cell per such rune, collapsing the layout (content
// crammed left, panel/scrollbar stranded far right). Probing the border we
// actually rely on makes the flag match how our real output is rendered.
const probeGlyph = "│"

// detectAmbiguousWidth decides, once at startup, whether the terminal draws
// East-Asian ambiguous-width runes as two cells and records it via
// setAmbiguousWide. Resolution order:
//
//	MAGI_AMBIGUOUS_WIDTH=wide|narrow   explicit override, always wins
//	RUNEWIDTH_EASTASIAN=1|0            the go-runewidth env knob, honored for parity
//	MAGI_WIDTH_PROBE=0                 disables the live probe
//	live probe on the tty              measures a real ambiguous glyph
//	(else)                            default narrow (unchanged behavior)
//
// The probe itself is platform-specific (probeAmbiguousWidth): a Console-API
// cursor delta on Windows, a Cursor-Position-Report round-trip elsewhere. Both
// are best-effort and defensive — any uncertainty leaves the default (narrow).
func detectAmbiguousWidth() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_AMBIGUOUS_WIDTH"))) {
	case "wide", "2", "double":
		setAmbiguousWide(true)
		return
	case "narrow", "1", "single":
		setAmbiguousWide(false)
		return
	}
	// RUNEWIDTH_EASTASIAN is go-runewidth's own knob (x/ansi's grapheme width does
	// NOT read it, so setting it alone wouldn't fix our layout); honor it here so a
	// user who already exports it gets the matching behavior in magi too.
	if ea, err := strconv.ParseBool(os.Getenv("RUNEWIDTH_EASTASIAN")); err == nil {
		setAmbiguousWide(ea)
		return
	}
	if os.Getenv("MAGI_WIDTH_PROBE") == "0" {
		return
	}
	if w, ok := probeAmbiguousWidth(os.Stdout, os.Stdin); ok {
		setAmbiguousWide(w == 2)
	}
}

// detectDecorWidths decides, once at startup, the real cell width of the
// decorative glyphs (decorGlyphs) that measure 1 in Unicode tables but may render
// wide (see width.go). Resolution mirrors detectAmbiguousWidth:
//
//	MAGI_DECOR_WIDTH=wide|narrow   explicit override, always wins
//	MAGI_WIDTH_PROBE=0             disables the live probe
//	per-glyph probe on the tty     measures each glyph's real width
//	(else)                         default narrow (unchanged behavior)
//
// It is best-effort: any uncertainty leaves decorWide unset (no correction). Call
// after detectAmbiguousWidth so both share the one-shot startup raw-mode window.
func detectDecorWidths() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_DECOR_WIDTH"))) {
	case "wide", "2", "double":
		m := make(map[rune]bool, len(decorGlyphs))
		for _, r := range decorGlyphs {
			m[r] = true
		}
		setDecorWide(m)
		return
	case "narrow", "1", "single":
		setDecorWide(map[rune]bool{})
		return
	}
	if os.Getenv("MAGI_WIDTH_PROBE") == "0" {
		return
	}
	if m, ok := probeDecorWidths(os.Stdout, os.Stdin); ok {
		setDecorWide(m)
	}
}
