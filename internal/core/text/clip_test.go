package text

import "strings"

import "testing"

// The property every copy of this existed for: a budget is in bytes, and a byte budget lands in the
// middle of a character often enough that "often enough" is every CJK string.
func TestClippingNeverSplitsACharacter(t *testing.T) {
	const s = "한국어 텍스트" // three bytes per syllable
	for n := 0; n <= len(s)+2; n++ {
		got := Cut(s, n)
		if len(got) > n && n <= len(s) {
			t.Errorf("Cut(%d) returned %d bytes", n, len(got))
		}
		if !isValidUTF8(got) {
			t.Errorf("Cut(%d) = %q, which is not valid UTF-8", n, got)
		}
		if !strings.HasPrefix(s, got) {
			t.Errorf("Cut(%d) = %q, which is not a prefix", n, got)
		}
	}
}

// A marker means "something is missing". Adding one to a string that fits says a thing that is not
// true, and the callers put these into prompts a model reasons about.
func TestTheMarkerAppearsOnlyWhenSomethingWasRemoved(t *testing.T) {
	for _, tc := range []struct{ s, want string }{
		{"short", "short"},
		{"exactly-8", "exactly-…"}, // nine bytes, so eight of them plus the mark
	} {
		if got := Clip(tc.s, 8); got != tc.want {
			t.Errorf("Clip(%q, 8) = %q, want %q", tc.s, got, tc.want)
		}
	}
	if got := ClipWith("a long line here", 6, "\n…(cut)"); got != "a long\n…(cut)" {
		t.Errorf("ClipWith = %q", got)
	}
	if got := ClipWith("fits", 40, "\n…(cut)"); got != "fits" {
		t.Errorf("a string that fits got a marker: %q", got)
	}
}

// Every one of the eight copies panicked here: `s[:-1]`. None could be reached with a negative
// budget today, which is exactly why it would have been found by whoever first could.
func TestANegativeBudgetIsEmpty(t *testing.T) {
	for _, n := range []int{-1, -100} {
		if got := Cut("hello", n); got != "" {
			t.Errorf("Cut(%d) = %q", n, got)
		}
		if got := Clip("hello", n); got != "…" {
			t.Errorf("Clip(%d) = %q", n, got)
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' && !strings.Contains(s, "�") {
			return false
		}
	}
	return len([]rune(s)) > 0 || s == ""
}
