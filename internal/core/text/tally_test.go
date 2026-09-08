package text

import "testing"

// The spelling this owns, stated once. Two screens read it — the header badge and the compaction
// brief — and every part of it is something that could have been written differently on each side.
func TestTallySpellsRepeatsTheSameWayEverywhere(t *testing.T) {
	for _, c := range []struct {
		what string
		in   []string
		sep  string
		want string
	}{
		{"first-seen order, not alphabetical", []string{"read", "edit", "edit", "bash"}, " · ", "read · edit×2 · bash"},
		{"the badge's separator", []string{"explore", "coder", "explore", "explore"}, ", ", "explore×3, coder"},
		{"a single occurrence carries no count", []string{"bash"}, " · ", "bash"},
		{"nothing in, nothing out", nil, " · ", ""},
		{"the repeat marker is ×, not x", []string{"a", "a"}, "", "a×2"},
		{"a later repeat does not move the first sighting", []string{"c", "a", "b", "a"}, ",", "c,a×2,b"},
	} {
		if got := Tally(c.in, c.sep); got != c.want {
			t.Errorf("%s: Tally(%v, %q) = %q, want %q", c.what, c.in, c.sep, got, c.want)
		}
	}
}
