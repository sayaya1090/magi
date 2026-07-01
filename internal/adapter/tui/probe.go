package tui

import (
	"os"
	"strconv"
	"strings"
)

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
