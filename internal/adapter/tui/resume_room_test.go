package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

// openPicker puts the session picker on screen with n sessions. The list is set directly rather
// than by creating n real sessions on disk: what is under test is the view's arithmetic against
// the terminal, and twenty sessions on disk take a second to make and prove nothing extra.
func openPicker(t *testing.T, w, h, n int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	list := make([]session.SessionMeta, n)
	for i := range list {
		list[i] = session.SessionMeta{
			Title:        fmt.Sprintf("session %02d — a title of ordinary length", i),
			LastActivity: time.Now().Add(-time.Duration(i) * time.Minute),
		}
	}
	s.m.resumeList, s.m.resuming, s.m.resumeSel = list, true, 0
	s.m.refresh()
	return s
}

// The picker shows twelve rows because twelve is the constant, not because twelve fit. It never
// asked the terminal: on a fourteen-row screen it draws its header, twelve sessions and a position
// line into eight rows of room, and on an alt screen the header and the top of the list are simply
// gone. Every other surface in this slot windows itself against modalRoom.
//
// This is the ninth surface in this package that reserved a COUNT instead of measuring.
func TestTheSessionPickerFitsItsTerminal(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{80, 14, 20}, {80, 12, 14}, {100, 16, 30}, {60, 10, 13}, {100, 40, 20}} {
		s := openPicker(t, c.w, c.h, c.n)
		if got, room := lipgloss.Height(s.m.resumeView()), s.m.modalRoom(); room > 0 && got > room {
			t.Errorf("w=%d h=%d n=%d: the picker draws %d rows into %d of room",
				c.w, c.h, c.n, got, room)
		}
		lines := strings.Split(s.rawView(), "\n")
		if len(lines) > c.h {
			t.Errorf("w=%d h=%d n=%d: the frame is %d rows", c.w, c.h, c.n, len(lines))
		}
		for i, l := range lines {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
			if got := lipgloss.Width(trimmed); got > c.w {
				t.Errorf("w=%d h=%d n=%d: row %d draws %d cells: %q", c.w, c.h, c.n, i, got, trimmed)
			}
		}
	}
}

// The session being chosen is on screen wherever it is in the list — the picker exists to answer
// "which one was I just in", and a window that cannot follow the cursor cannot answer it.
func TestTheChosenSessionStaysOnScreen(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{80, 14, 20}, {80, 30, 40}, {100, 16, 25}} {
		for _, sel := range []int{0, c.n / 2, c.n - 1} {
			s := openPicker(t, c.w, c.h, c.n)
			s.m.resumeSel = sel
			out := ansiSeq.ReplaceAllString(s.m.resumeView(), "")
			if want := fmt.Sprintf("session %02d", sel); !strings.Contains(out, want) {
				t.Errorf("w=%d h=%d n=%d sel=%d: %q is not drawn:\n%s", c.w, c.h, c.n, sel, want, out)
			}
		}
	}
}

// A cut list says how far into it the cursor is; a list that fits says nothing.
func TestACutPickerSaysWhereTheCursorIs(t *testing.T) {
	applyTheme(true)
	out := ansiSeq.ReplaceAllString(openPicker(t, 80, 14, 20).m.resumeView(), "")
	if !strings.Contains(out, "/20") {
		t.Errorf("twenty sessions were cut to a screenful with no position shown:\n%s", out)
	}
	full := ansiSeq.ReplaceAllString(openPicker(t, 100, 40, 4).m.resumeView(), "")
	if strings.Contains(full, "/4") {
		t.Errorf("a picker showing every session is marked as cut:\n%s", full)
	}
	for i := 0; i < 4; i++ {
		if !strings.Contains(full, fmt.Sprintf("session %02d", i)) {
			t.Errorf("session %02d is missing from a picker with room for all of them:\n%s", i, full)
		}
	}
}

// The reserve and the render agree. They are computed in different places — the reserve counted
// rows from a constant while the view drew from the same constant, which agreed only for as long
// as neither consulted the screen.
func TestThePickerReserveMatchesWhatItDraws(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{80, 14, 20}, {100, 16, 30}, {100, 40, 20}, {80, 12, 6}} {
		s := openPicker(t, c.w, c.h, c.n)
		if got := s.m.chromeHeight() + s.m.vp.Height(); got > c.h {
			t.Errorf("w=%d h=%d n=%d: chrome %d + viewport %d = %d rows",
				c.w, c.h, c.n, s.m.chromeHeight(), s.m.vp.Height(), got)
		}
	}
}
