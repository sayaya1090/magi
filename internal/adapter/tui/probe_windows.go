//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// probeAmbiguousWidth measures the real cell width of an ambiguous glyph
// (probeGlyph, the panel border │) on a Windows console by reading the cursor
// column before and after printing it via the Console API — no stdin, no raw
// mode, no CPR round-trip
// (Windows console input handles aren't pollable, so the CPR path can't run
// there). Returns (width, true) only for a plausible 1- or 2-cell result on a
// real console; a redirected handle or wrapped line falls back to narrow. The
// unused `in` keeps the cross-platform signature.
func probeAmbiguousWidth(out, in *os.File) (int, bool) {
	if out == nil {
		return 0, false
	}
	h := windows.Handle(out.Fd())
	var before windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &before); err != nil {
		return 0, false // not a real console (redirected/piped)
	}
	if _, err := out.WriteString(probeGlyph); err != nil {
		return 0, false
	}
	var after windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &after); err != nil {
		return 0, false
	}
	w := int(after.CursorPosition.X - before.CursorPosition.X)
	// Best-effort cleanup: return to the start and overwrite the glyph.
	_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
	_, _ = out.WriteString("  ")
	_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
	if w < 1 || w > 2 { // negative means the glyph wrapped the line end; ignore
		return 0, false
	}
	return w, true
}

// probeEmojiWidth measures the real cell width of emojiProbeGlyph via the same
// Console-API cursor-delta method as probeAmbiguousWidth. Returns (width, true)
// only for a plausible 1- or 2-cell result on a real console; a redirected handle
// or wrapped line falls back to (0,false) so the default (wide) stands. The unused
// `in` keeps the cross-platform signature.
func probeEmojiWidth(out, in *os.File) (int, bool) {
	if out == nil {
		return 0, false
	}
	h := windows.Handle(out.Fd())
	var before windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &before); err != nil {
		return 0, false // not a real console (redirected/piped)
	}
	if _, err := out.WriteString(emojiProbeGlyph); err != nil {
		return 0, false
	}
	var after windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &after); err != nil {
		return 0, false
	}
	w := int(after.CursorPosition.X - before.CursorPosition.X)
	// Best-effort cleanup: return to the start and overwrite the glyph.
	_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
	_, _ = out.WriteString("  ")
	_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
	if w < 1 || w > 2 { // negative means the glyph wrapped the line end; ignore
		return 0, false
	}
	return w, true
}

// probeDecorWidths measures each decorative glyph (decorGlyphs) via the same
// Console-API cursor-delta method as probeAmbiguousWidth. Returns a rune→isWide
// map; a redirected handle or an implausible delta abandons the batch (ok=false)
// so the default (narrow) stands. The unused `in` keeps the cross-platform
// signature.
func probeDecorWidths(out, in *os.File) (map[rune]bool, bool) {
	if out == nil {
		return nil, false
	}
	h := windows.Handle(out.Fd())
	m := make(map[rune]bool, len(decorGlyphs))
	for _, r := range decorGlyphs {
		var before windows.ConsoleScreenBufferInfo
		if err := windows.GetConsoleScreenBufferInfo(h, &before); err != nil {
			return nil, false
		}
		if _, err := out.WriteString(string(r)); err != nil {
			return nil, false
		}
		var after windows.ConsoleScreenBufferInfo
		if err := windows.GetConsoleScreenBufferInfo(h, &after); err != nil {
			return nil, false
		}
		w := int(after.CursorPosition.X - before.CursorPosition.X)
		// Best-effort cleanup: return to the start and overwrite the glyph.
		_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
		_, _ = out.WriteString("  ")
		_ = windows.SetConsoleCursorPosition(h, before.CursorPosition)
		if w < 1 || w > 2 {
			return nil, false
		}
		if w == 2 {
			m[r] = true
		}
	}
	return m, true
}
