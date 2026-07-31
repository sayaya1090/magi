package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ctrlF opens the transcript search bar and types a query into it.
func openSearch(s *script, query string) {
	s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	for _, r := range query {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// The find bar is a single row of fixed text — three key hints run ~40 cells before the query
// even starts — and it drew all of them regardless of the terminal. In a vertically joined
// frame one over-wide row pads every other row to match, so a narrow window did not just clip
// the bar: the whole screen went wider than the terminal and the shell wrapped it.
func TestTheFindBarFitsANarrowTerminal(t *testing.T) {
	for _, w := range []int{28, 43, 60, 100} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 24})
		s.steer("r1", "something to search for")
		s.assistantText("the answer mentions the thing")
		openSearch(s, "the")

		if got := lipgloss.Width(s.m.searchView()); got > w {
			t.Errorf("width %d: the find bar draws %d cells: %q", w, got, ansiSeq.ReplaceAllString(s.m.searchView(), ""))
		}
		for i, line := range strings.Split(s.rawView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Fatalf("width %d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
	// The query and its match count are what survive the shrinking — the hints are the part
	// that is decoration, and "esc close" goes last because it is the way out.
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 30, Height: 24})
	s.steer("r1", "hello")
	openSearch(s, "hel")
	if bar := ansiSeq.ReplaceAllString(s.m.searchView(), ""); !strings.Contains(bar, "hel") {
		t.Errorf("the query itself was dropped: %q", bar)
	}
}

// Opening search adds a row to the frame. Every other surface drawn there — the palette, the
// resume list, the route editor, both modals — reserves its rows in baseChromeHeight; the find
// bar reserved nothing, so the frame came out one row taller than the terminal. On an alt-screen
// UI that does not scroll: the top row is gone.
func TestOpeningSearchDoesNotPushTheFrameOffTheScreen(t *testing.T) {
	for _, h := range []int{12, 24, 33} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: h})
		s.steer("r1", "fill the transcript")
		for i := 0; i < 20; i++ {
			s.assistantText("a line worth searching")
		}
		before := len(strings.Split(s.rawView(), "\n"))
		openSearch(s, "line")
		rows := len(strings.Split(s.rawView(), "\n"))
		if rows > h {
			t.Errorf("height %d: the frame is %d rows with search open (was %d closed, chrome=%d vp=%d)",
				h, rows, before, s.m.chromeHeight(), s.m.vp.Height())
		}
	}
}

// A hit is a LINE INDEX, and line numbering is not stable: a resize rewraps the whole
// transcript. The list was built when the query was typed and never rebuilt, so afterwards it
// pointed into a transcript that no longer had those lines — "next match" scrolled to nothing
// and the "3/7" counter counted matches that were not there.
func TestSearchHitsSurviveAResize(t *testing.T) {
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	s.steer("r1", "find the needle")
	for i := 0; i < 30; i++ {
		s.assistantText("a padding line that is long enough to wrap when the terminal narrows, needle included")
	}
	openSearch(s, "needle")
	if len(s.m.searchHits) == 0 {
		t.Fatal("the query matched nothing, so this test would prove nothing")
	}

	for _, w := range []int{40, 120, 55} {
		s.send(tea.WindowSizeMsg{Width: w, Height: 30})
		_ = s.rawView()
		if len(s.m.searchHits) == 0 {
			t.Fatalf("width %d: every hit was lost", w)
		}
		for _, h := range s.m.searchHits {
			if h < 0 || h >= len(s.m.contentPlain) {
				t.Fatalf("width %d: hit %d is outside the %d-line transcript", w, h, len(s.m.contentPlain))
			}
			if !strings.Contains(strings.ToLower(s.m.contentPlain[h]), "needle") {
				t.Errorf("width %d: hit %d points at a line with no match: %q", w, h, s.m.contentPlain[h])
			}
		}
		if s.m.searchCur >= len(s.m.searchHits) {
			t.Errorf("width %d: the current match is %d of %d", w, s.m.searchCur, len(s.m.searchHits))
		}
	}
}

// The transcript also grows while search is open — the turn keeps talking. Same failure, no
// resize needed: hits computed over a shorter transcript, then more blocks arrive.
func TestSearchHitsFollowATranscriptThatKeepsGrowing(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "watch it grow")
	s.assistantText("the first needle")
	openSearch(s, "needle")
	first := len(s.m.searchHits)
	if first == 0 {
		t.Fatal("nothing matched to begin with")
	}
	for i := 0; i < 5; i++ {
		s.assistantText("another needle arrives")
	}
	_ = s.rawView()
	if len(s.m.searchHits) <= first {
		t.Errorf("five more matches arrived and the count stayed at %d → %d", first, len(s.m.searchHits))
	}
	for _, h := range s.m.searchHits {
		if h < 0 || h >= len(s.m.contentPlain) {
			t.Fatalf("hit %d is outside the %d-line transcript", h, len(s.m.contentPlain))
		}
	}
}
