package text

import "testing"

// One name per behaviour, and the differences between them are what this asserts.
//
// Before these lived here, `firstLine` existed in four packages and `oneLine` in four, and each
// name covered several behaviours. The rows below are exactly the places they disagreed — so a
// caller reading a name gets the one thing it does, and an edit that quietly makes one behave
// like its neighbour is a failure rather than a subtle change of output on somebody's screen.
func TestEachWayOfMakingOneLineKeepsItsOwnRule(t *testing.T) {
	for _, c := range []struct {
		what string
		got  string
		want string
	}{
		// FirstLine: cut at the newline, then trim. It does NOT skip a leading blank line — the
		// first line of "\nx" is the blank one, and a caller that wants otherwise says so.
		{"first of several", FirstLine("a\nb\nc"), "a"},
		{"no newline at all", FirstLine("solo"), "solo"},
		{"trims what it kept", FirstLine("  hello  \nworld"), "hello"},
		{"a CRLF line keeps no carriage return", FirstLine("a\r\nb"), "a"},
		{"a leading blank line IS the first line", FirstLine("\nx"), ""},

		// FirstLineClipped: the same line, cut to n runes with an ellipsis.
		{"under the budget is untouched", FirstLineClipped("abc", 3), "abc"},
		{"over the budget gets the mark", FirstLineClipped("abcdef", 3), "abc…"},
		{"runes, not bytes", FirstLineClipped("가나다라", 2), "가나…"},

		// Collapse: every run of whitespace becomes one space.
		{"newlines, tabs and doubles all collapse", Collapse("a\n\tb   c"), "a b c"},
		{"and the ends are clean", Collapse("  a  "), "a"},

		// Unwrap: only the LINES join; spacing inside a line survives, which is the whole
		// difference from Collapse and the reason both exist.
		{"lines join with one space", Unwrap("a\nb"), "a b"},
		{"inner alignment survives", Unwrap("a\tb  c"), "a\tb  c"},
		{"a CR line ending joins too", Unwrap("a\r\nb"), "a b"},

		// Nothing in, nothing out — for all four.
		{"FirstLine of nothing", FirstLine(""), ""},
		{"FirstLineClipped of nothing", FirstLineClipped("", 5), ""},
		{"Collapse of nothing", Collapse("   "), ""},
		{"Unwrap of nothing", Unwrap("\n\n"), ""},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.what, c.got, c.want)
		}
	}
}
