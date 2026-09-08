package text

import "strings"

// Four ways to make one line out of many, each with its own name.
//
// # Why they are named rather than shared
//
// This tree had `firstLine` in four packages and `oneLine` in four, and neither name meant one
// thing. `firstLine` was "cut at the newline, then trim" in two places, "cut, do not trim" in a
// third (whose one caller wrapped the call in TrimSpace to make up for it), and "trim, cut, clip
// to n runes with an ellipsis" in a fourth. `oneLine` was "collapse every run of whitespace" in
// two places and "join only on newlines, keeping the tabs and double spaces" in another.
//
// Two names for six behaviours is worse than six copies: copies drift, but a name that already
// means three things sends somebody who read it in one file to the wrong behaviour in the next.
// So the names say what they do, and the choice is made at the call site.
//
// The TUI's own `oneLine(s, max)` is deliberately NOT here. It replaces newlines, strips terminal
// control sequences and clips — and the stripping is a security control, the choke point for the
// preview and header paths that untrusted model and tool text reaches. It belongs beside the
// other rendering guards that share `stripControl`, not in a general string package where the
// next caller would take it for a formatting helper.

// FirstLine is everything before the first newline, trimmed.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// FirstLineClipped is FirstLine cut to n runes, with an ellipsis when anything was dropped.
// Runes, not bytes: a budget spent mid-character prints a replacement glyph.
func FirstLineClipped(s string, n int) string {
	s = FirstLine(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Collapse turns every run of whitespace — newlines, tabs, repeated spaces — into one space.
// For text going somewhere that has room for a phrase but not for its shape.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Unwrap joins the LINES of s with single spaces and leaves the rest of the spacing alone, so an
// indented list or an aligned column keeps its shape within the line. Trimmed at both ends.
func Unwrap(s string) string {
	return strings.TrimSpace(strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r'
	}), " "))
}
