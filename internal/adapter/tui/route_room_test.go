package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/app"
)

// openRoute opens the models & routing editor on a sized terminal.
func openRoute(t *testing.T, w, h int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	s.assistantText("some work happened")
	s.m.openRouteEditor()
	s.m.refresh()
	if !s.m.routing || len(s.m.routeList) == 0 {
		t.Fatal("the editor did not open, so nothing here is under test")
	}
	return s
}

// The editor's header was bounded in width — for a split pane, a phone-sized ssh window — and its
// height was never bounded at all. It lists the session model, every agent override and every
// profile, and drew all of them: eleven rows of chrome on a ten-row terminal, where an alt screen
// puts the title and the selected row above the display.
//
// Its WIDTH was pinned in route_width_test.go; this is the other axis, which that pass left open.
// Every other surface drawn in this slot windows itself against modalRoom. This one is the last.
func TestTheRouteEditorFitsAShortTerminal(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h int }{{40, 10}, {30, 8}, {60, 12}, {80, 14}, {100, 40}} {
		s := openRoute(t, c.w, c.h)
		if got, room := lipgloss.Height(s.m.routeView()), s.m.modalRoom(); got > room && room > 0 {
			t.Errorf("w=%d h=%d: the editor draws %d rows into %d of room", c.w, c.h, got, room)
		}
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

// A terminal with room for everything is untouched — the row count is not a cut.
func TestARouteEditorThatFitsCarriesNoMarker(t *testing.T) {
	applyTheme(true)
	s := openRoute(t, 100, 40)
	out := ansiSeq.ReplaceAllString(s.m.routeView(), "")
	if strings.Contains(out, "rows (↑/↓") {
		t.Errorf("an editor with room for every row is marked as cut:\n%s", out)
	}
	if !strings.Contains(out, "+ add profile") {
		t.Errorf("the last row is missing from an editor that fits:\n%s", out)
	}
}

// The popup drawn in the same slot had a `room < 3` arm that returned the UNBOUNDED list, so the
// bound switched off on exactly the terminals that needed it: at eight rows it drew 55.
func TestTheCompletionPopupIsBoundedOnAShortTerminal(t *testing.T) {
	applyTheme(true)
	for _, h := range []int{8, 9, 10, 12} {
		s := openPalette(t, 60, h)
		if got := lipgloss.Height(s.m.paletteView(s.m.paletteMatches())); got > s.m.modalRoom() {
			t.Errorf("h=%d: the popup draws %d rows into %d of room",
				h, got, s.m.modalRoom())
		}
	}
}

// …and its cut marker is a line like any other: "  … 20 commands (type to narrow)" is 33 cells
// however narrow the terminal is, and a marker that reports a cut by overflowing the screen is
// the defect it was added to report.
func TestTheCutMarkerFitsANarrowTerminal(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{20, 21, 23, 26, 30} {
		s := openPalette(t, w, 10)
		for i, l := range strings.Split(s.m.paletteView(s.m.paletteMatches()), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("w=%d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
}

// The row being edited has to be on screen. The editor windows its rows when the terminal is
// short, and the window is centred on the selection — but nothing held it to that: a mutation
// pinning the window to `start = 0` passed the whole suite. The question modal and the session
// picker each got this assertion when they were fixed; the editor, fixed in the same hour, did
// not, and the gap surfaced only under a mutation run.
//
// A cursor the window cannot follow is worse on this surface than on the others: the row it loses
// is the one the user is typing a model name into.
func TestTheSelectedRouteRowStaysOnScreen(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h int }{{60, 12}, {80, 14}, {100, 16}} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: c.w, Height: c.h})
		s.assistantText("some work happened")
		// Enough profiles that the list CANNOT fit — with the two rows a bare app has, the
		// editor never windows and the cursor has nowhere to fall off. The first version of this
		// test missed the mutation for exactly that reason: it was green over an unwindowed list.
		for i := 0; i < 12; i++ {
			s.m.app.SetProfile(app.ProfileDef{Name: fmt.Sprintf("prof%02d", i), Model: "m"})
		}
		s.m.openRouteEditor()
		s.m.refresh()
		if !s.m.routing || len(s.m.routeList) < 10 {
			t.Fatalf("the editor has %d rows; too few to window", len(s.m.routeList))
		}
		// Walk to the last row the way a user does, then look for it.
		for i := 0; i < len(s.m.routeList)-1; i++ {
			s.m.handleRouteKey(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		last := s.m.routeList[len(s.m.routeList)-1]
		out := ansiSeq.ReplaceAllString(s.m.routeView(), "")
		want := strings.TrimPrefix(last.name, "profile:")
		if len(want) > 14 {
			want = want[:14] // the row is clipped on a narrow terminal; match what fits
		}
		if !strings.Contains(out, want) {
			t.Errorf("w=%d h=%d: the selected row %q is not drawn:\n%s", c.w, c.h, want, out)
		}
	}
}
