package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestDegenerateRepeat(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want bool // whether a repetition loop is detected
	}{
		{"sentence repeated", strings.Repeat("The server is now running on port 5328. ", 6), true},
		{"short phrase repeated", strings.Repeat("the ", 60), true},
		{"single char run", strings.Repeat("a", 200), true},
		{"normal prose", "The quick brown fox jumps over the lazy dog. " +
			"A completely ordinary paragraph of varied text that does not loop at all, continuing with more words.", false},
		{"blank lines only (not content)", strings.Repeat("\n", 200), false},
		{"too short to judge", "the the the", false},
		{"few reps below threshold", strings.Repeat("hello world ", 2), false}, // 24 bytes < repMinBlock
	}
	for _, c := range cases {
		got := degenerateRepeat([]byte(c.tail)) > 0
		if got != c.want {
			t.Errorf("%s: degenerateRepeat=%v, want %v", c.name, got, c.want)
		}
	}
}

// A non-repeating tail must stay cheap: each candidate period mismatches at the first comparison.
func TestDegenerateRepeatNoFalsePositiveOnVariedText(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		// The line number makes the content non-periodic (no repeating block), unlike a fixed cycle.
		fmt.Fprintf(&b, "line %d: %s done\n", i, strings.Repeat("x", i%13))
	}
	if p := degenerateRepeat([]byte(b.String())); p > 0 {
		t.Errorf("varied text falsely flagged as repetition (period %d)", p)
	}
}
