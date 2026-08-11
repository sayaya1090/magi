package text_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/text"
)

// The property every copy of this existed for: a budget is in bytes, and a byte budget lands in the
// middle of a character often enough that "often enough" is every CJK string.
func TestClippingNeverSplitsACharacter(t *testing.T) {
	const s = "한국어 텍스트" // three bytes per syllable
	for n := 0; n <= len(s)+2; n++ {
		got := text.Cut(s, n)
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
		if got := text.Clip(tc.s, 8); got != tc.want {
			t.Errorf("Clip(%q, 8) = %q, want %q", tc.s, got, tc.want)
		}
	}
	if got := text.ClipWith("a long line here", 6, "\n…(cut)"); got != "a long\n…(cut)" {
		t.Errorf("ClipWith = %q", got)
	}
	if got := text.ClipWith("fits", 40, "\n…(cut)"); got != "fits" {
		t.Errorf("a string that fits got a marker: %q", got)
	}
}

// Every one of the eight copies panicked here: `s[:-1]`. None could be reached with a negative
// budget today, which is exactly why it would have been found by whoever first could.
func TestANegativeBudgetIsEmpty(t *testing.T) {
	for _, n := range []int{-1, -100} {
		if got := text.Cut("hello", n); got != "" {
			t.Errorf("Cut(%d) = %q", n, got)
		}
		if got := text.Clip("hello", n); got != "…" {
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

// Big output keeps its ends and says what it dropped from the middle.
//
// Head-only clipping is wrong for anything a machine produced: a build log's error and its final
// status live at the END, so cutting the tail leaves the part where everything was still going
// fine — which reads as a run that simply stopped.
func TestHeadTailKeepsBothEndsAndSaysWhatWent(t *testing.T) {
	head := strings.Repeat("H", 4000)
	tail := "FAILED: 3 tests"
	s := head + strings.Repeat("m", 60000) + tail
	got := text.HeadTail(s, 8000)

	if len(got) >= len(s) {
		t.Fatalf("nothing was elided: %d of %d", len(got), len(s))
	}
	if !strings.HasPrefix(got, "HHHH") {
		t.Error("the beginning went")
	}
	if !strings.HasSuffix(got, tail) {
		t.Errorf("the end went, which is where the answer is: …%q", got[max(0, len(got)-40):])
	}
	// How much, not just that. "…" leaves a reader unable to tell six characters from six
	// megabytes, which is the difference between a line that was tidied and a log that was gutted.
	if !strings.Contains(got, "bytes omitted") {
		t.Errorf("it does not say how much it dropped:\n%s", got[:120])
	}
	if !utf8.ValidString(got) {
		t.Error("the cut landed inside a character")
	}
}

// A budget smaller than the two halves keeps the head and still says what went.
func TestHeadTailUnderATinyBudgetStillAccountsForItself(t *testing.T) {
	got := text.HeadTail(strings.Repeat("가", 100), 8)
	if !strings.Contains(got, "omitted") {
		t.Errorf("a tiny budget dropped everything silently: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("the cut landed inside a character: %q", got)
	}
}

// What fits is passed through untouched — no marker on something that lost nothing.
func TestHeadTailLeavesWhatFitsAlone(t *testing.T) {
	s := "a short result"
	if got := text.HeadTail(s, 8000); got != s {
		t.Errorf("a short result came back as %q", got)
	}
}
