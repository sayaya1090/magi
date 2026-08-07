// Package text holds the string operations several layers need and none of them owns.
//
// It exists because one of them — cutting a string to a byte budget without splitting a rune — had
// been written eight times: in the council, in the event payloads, three times in the council
// evidence builder, in the workflow, in the fleet view, and in the CLI. Every copy was the same
// four lines with a different marker glued on the end, and every copy panicked on a negative
// budget ("slice bounds out of range [:-1]" — verified, not assumed). None of them can be reached
// with one today; the point is that the fix reaches all eight at once, which is the thing eight
// copies of a primitive cost.
//
// In core because core imports nothing outside the standard library and core, and two of the eight
// copies live there.
package text

import "unicode/utf8"

// Cut returns at most n bytes of s, ending on a rune boundary, with no marker.
//
// Bytes rather than runes because the callers are budgeting a prompt or a wire field, and those are
// measured in bytes. A cut in the middle of a multibyte character produces invalid UTF-8, which a
// terminal draws as a replacement glyph and a JSON encoder rejects outright — so the boundary is
// walked back to, never rounded off.
func Cut(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Clip is Cut with an ellipsis when something was actually removed.
func Clip(s string, n int) string { return ClipWith(s, n, "…") }

// ClipWith is Cut with a caller's marker when something was actually removed.
//
// The marker is the only thing that ever differed between the copies, and it differs for a reason
// worth keeping: a bare "…" is right for a line on a dashboard and wrong for a spec a model is told
// to reproduce verbatim, where the ellipsis gets copied into an edit's old-string and matches
// nothing. Whoever is clipping knows which; this does not guess.
func ClipWith(s string, n int, marker string) string {
	if n >= len(s) {
		return s
	}
	return Cut(s, n) + marker
}
