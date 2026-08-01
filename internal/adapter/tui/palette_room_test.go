package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openPalette types the slash that opens the completion popup, on a session with a transcript so
// the splash is not covering the viewport.
func openPalette(t *testing.T, w, h int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	s.assistantText("some work happened")
	s.typeText("/")
	if len(s.m.paletteMatches()) == 0 {
		t.Fatal("no matches, so the palette is not under test")
	}
	return s
}

// The slash-command popup was unbounded, and its reserve counted MATCHES while its renderer drew
// LINES — a long "/name   description" wraps inside the box on a narrow terminal. Twenty commands
// reserved 22 rows and drew 55 at 34 columns; the frame ran past the terminal by exactly that
// difference, and on an alt-screen UI the top of the frame is simply gone.
//
// Both halves are fixed here: the reserve asks the view what it draws, and the view shrinks to the
// room this slot has. This is the sixth surface in this package with the same shape.
func TestThePaletteFitsItsTerminal(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h int }{{34, 30}, {44, 30}, {60, 30}, {80, 30}, {100, 30}, {140, 50}, {80, 14}} {
		s := openPalette(t, c.w, c.h)
		lines := strings.Split(s.rawView(), "\n")
		if len(lines) > c.h {
			t.Errorf("w=%d h=%d: the frame is %d rows", c.w, c.h, len(lines))
		}
		for i, l := range lines {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
			if got := lipgloss.Width(trimmed); got > c.w {
				t.Errorf("w=%d h=%d: row %d draws %d cells: %q", c.w, c.h, i, got, trimmed)
			}
		}
	}
}

// A list cut short says how many it dropped. Silently ending at the tenth command reads as a magi
// with ten commands.
func TestACutPaletteSaysHowManyAreLeft(t *testing.T) {
	applyTheme(true)
	s := openPalette(t, 60, 30)
	out := ansiSeq.ReplaceAllString(s.rawView(), "")
	if !strings.Contains(out, "more (type to narrow)") {
		t.Errorf("a palette that did not fit was cut with nothing saying so:\n%s", out)
	}
	// …and a terminal with room for all of them carries no marker.
	wide := ansiSeq.ReplaceAllString(openPalette(t, 120, 40).rawView(), "")
	if strings.Contains(wide, "more (type to narrow)") {
		t.Errorf("a palette that fits is marked as cut:\n%s", wide)
	}
}

// The window follows the selection: the command being chosen is the one that has to be on screen,
// and a cut that always keeps the first N would hide it as soon as the user arrows down.
func TestTheSelectedCommandStaysOnScreen(t *testing.T) {
	applyTheme(true)
	s := openPalette(t, 60, 30)
	matches := s.m.paletteMatches()
	if len(matches) < 6 {
		t.Skipf("only %d matches; nothing to scroll", len(matches))
	}
	last := matches[len(matches)-1].name
	for i := 0; i < len(matches)-1; i++ {
		s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	out := ansiSeq.ReplaceAllString(s.rawView(), "")
	if !strings.Contains(out, last) {
		t.Errorf("the selected command %q is not on screen:\n%s", last, out)
	}
}

// The reserve and the render agree. They are computed in different places, and when they drifted
// the frame was exactly their difference too tall — so the check is that chrome plus viewport is
// the terminal, not one row more.
func TestThePaletteReserveMatchesWhatItDraws(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h int }{{34, 30}, {60, 30}, {100, 30}} {
		s := openPalette(t, c.w, c.h)
		if got := s.m.chromeHeight() + s.m.vp.Height(); got > c.h {
			t.Errorf("w=%d h=%d: chrome %d + viewport %d = %d rows",
				c.w, c.h, s.m.chromeHeight(), s.m.vp.Height(), got)
		}
	}
}
