package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Every search function is at 100% line coverage, and none of it was exercised where it does
// anything: the existing tests search transcripts shorter than the screen, so searchJump's scroll
// arithmetic clamps to zero on every call and the hit is already visible before the jump. Entry
// without consequence is the mirror of a green that never entered.
//
// This is the session the search is FOR — one long enough that the match is nowhere near the fold.
// Four things have to hold and none of them is checked by a hit count: the list must be the whole
// list, each step must land on a line that really contains the query, the landing line must be on
// screen after the jump, and the last step must wrap to the first rather than run off the end.
func TestSearchLandsOnItsMatchInALongSession(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})

	const needle = "zqneedle"
	want := 0
	for i := 0; i < 400; i++ {
		if i%50 == 0 {
			s.assistantText(fmt.Sprintf("block %d holds the %s right here", i, needle))
			want++
		} else {
			s.assistantText(fmt.Sprintf("block %d is ordinary filler text", i))
		}
	}
	s.rawView()
	if len(s.m.contentPlain) <= s.m.vp.Height() {
		t.Fatalf("the transcript is %d lines in a %d-row viewport — nothing here is below the fold",
			len(s.m.contentPlain), s.m.vp.Height())
	}

	s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	for _, r := range needle {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	s.rawView()
	if len(s.m.searchHits) != want {
		t.Fatalf("found %d matches in a transcript holding %d", len(s.m.searchHits), want)
	}

	// One full lap plus one, so the wrap is walked too.
	scrolled := 0
	seen := map[int]bool{}
	for k := 0; k <= want; k++ {
		// The extra step past the last match must have wrapped back to the first.
		if k == want && s.m.searchCur != 0 {
			t.Errorf("stepping past the last of %d matches landed on hit %d, not back at the first",
				want, s.m.searchCur)
		}
		h := s.m.searchHits[s.m.searchCur]
		if h >= len(s.m.contentPlain) {
			t.Fatalf("step %d: hit line %d is past the %d-line transcript", k, h, len(s.m.contentPlain))
		}
		line := s.m.contentPlain[h]
		if !strings.Contains(strings.ToLower(line), needle) {
			t.Errorf("step %d: jumped to line %d, which does not contain the query: %q", k, h, line)
		}
		off := s.m.vp.YOffset()
		if h < off || h >= off+s.m.vp.Height() {
			t.Errorf("step %d: line %d is off screen at offset %d (%d rows)", k, h, off, s.m.vp.Height())
		}
		if off > 0 {
			scrolled++
		}
		if k < want {
			seen[h] = true
		}
		s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		s.rawView()
	}
	if len(seen) != want {
		t.Errorf("a lap through the matches visited %d distinct lines, not %d", len(seen), want)
	}
	// If nothing ever scrolled, the jump was a no-op and this test proved what the short ones do.
	if scrolled == 0 {
		t.Error("no step required scrolling, so the jump arithmetic was never under test")
	}
}
