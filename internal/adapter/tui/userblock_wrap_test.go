package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// A user prompt in the transcript must (a) keep explicit newlines and (b) wrap a
// long line to the transcript width instead of overflowing off-screen. Regression
// for the clipped multi-line "you" block.
func TestUserBlockWrapsAndKeepsNewlines(t *testing.T) {
	applyTheme(true)
	m := &Model{}
	m.width = 60 // no panes → transcriptWidth ≈ 60

	// (a) explicit newlines survive.
	multi := m.renderBlockAs(block{kind: blockUser, text: "one\ntwo\nthree"}, "magi", nil)
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(multi, want) {
			t.Fatalf("multi-line user block dropped %q:\n%s", want, multi)
		}
	}

	// (b) a long single line wraps within the transcript width.
	long := strings.Repeat("verylongword ", 20)
	got := m.renderBlockAs(block{kind: blockUser, text: long}, "magi", nil)
	widest := 0
	for _, ln := range strings.Split(got, "\n") {
		if w := lipgloss.Width(ln); w > widest {
			widest = w
		}
	}
	if widest > m.transcriptWidth() {
		t.Fatalf("user block not wrapped: widest line %d > transcript width %d", widest, m.transcriptWidth())
	}
	if strings.Count(got, "\n") < 2 {
		t.Fatalf("long line should wrap to multiple rows, got:\n%s", got)
	}
}
