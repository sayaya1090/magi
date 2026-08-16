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

import (
	"fmt"
	"unicode/utf8"
)

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

// CutTail returns at most n bytes of the END of s, starting on a rune boundary, with no marker —
// Cut's mirror, for the callers whose information lives at the end (a completion prompt's prefix,
// where the code nearest the cursor matters and the file's opening lines do not).
func CutTail(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
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

// HeadTail keeps the beginning AND the end of s within n bytes, eliding the middle and saying how
// much it dropped.
//
// Head-only clipping is wrong for anything a machine produced. A build log's error and its final
// status live at the END — cut the tail and what survives is the part that was going fine, which
// reads as a run that simply stopped. Three quarters head to one quarter tail, because the head
// carries what was being done and the tail carries how it went, and the second is shorter.
//
// The count is part of the output, not a nicety. "…" says something was removed and leaves the
// reader unable to tell six characters from six megabytes, which is the difference between a line
// that was tidied and a log that was gutted.
//
// Cuts on rune boundaries, so the result is always valid UTF-8 — a half rune is a replacement glyph
// in a terminal and a rejected encode in JSON.
func HeadTail(s string, n int) string {
	if n <= 0 || len(s) <= n {
		if n <= 0 {
			return ""
		}
		return s
	}
	head := n * 3 / 4
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := len(s) - (n - head)
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	if tail <= head { // n so small the two halves meet; keep the head and say so
		return Cut(s, n) + omitted(len(s)-n)
	}
	return s[:head] + omitted(tail-head) + s[tail:]
}

// omitted is the marker the elision leaves behind, on its own lines so it cannot be mistaken for a
// line of the output it interrupts.
//
// An exact byte count, not a rounded one. The first reader of this is the model, which is deciding
// whether to go and fetch the rest; "34816" tells it how much it is missing and "34 KB" makes it
// guess. It costs five characters.
func omitted(n int) string { return fmt.Sprintf("\n…(%d bytes omitted)…\n", n) }
