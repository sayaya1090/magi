package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

// planOf builds n plan steps as the app records them. The panel reads the APP's todos, not an
// event the model saw — feeding it through the event stream leaves the panel empty and every
// assertion about it vacuous, which is how the first pass at this test proved nothing.
func planOf(n int) []session.Todo {
	var td []session.Todo
	for i := 0; i < n; i++ {
		st := "pending"
		switch {
		case i == 0:
			st = "in_progress"
		case i < 3:
			st = "completed"
		}
		td = append(td, session.Todo{Content: fmt.Sprintf("step %02d do the thing", i), Status: st})
	}
	return td
}

func withPlan(t *testing.T, w, h, n int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	s.assistantText("working")
	s.m.app.SetTodos(s.m.sid, planOf(n))
	s.m.refresh()
	return s
}

// The overview panel had no vertical bound: it built every plan step, every observation and every
// background row, and floatPanel then refused to draw a box taller than the screen — so the panel
// VANISHED. Five steps rendered and twenty-five at the same size rendered nothing, which means the
// panel a user watches a long task through is the one a long task removes.
//
// The worker panel next door already clips to exactly this room. Only the overview lacked it.
func TestALongPlanStillHasAPanel(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{100, 24, 25}, {100, 40, 40}, {120, 30, 60}} {
		out := ansiSeq.ReplaceAllString(withPlan(t, c.w, c.h, c.n).rawView(), "")
		if !strings.Contains(out, "Plan  ") {
			t.Errorf("w=%d h=%d with %d steps: the panel is gone entirely:\n%s", c.w, c.h, c.n, out)
		}
		if !strings.Contains(out, "more rows") {
			t.Errorf("w=%d h=%d with %d steps: the panel was cut with nothing saying so", c.w, c.h, c.n)
		}
	}
}

// A plan that fits is untouched — no marker, every step on screen. The marker is the price of the
// cut being visible, and charging it where nothing was cut is noise in a panel meant to be glanced
// at.
func TestAPlanThatFitsCarriesNoMarker(t *testing.T) {
	applyTheme(true)
	out := ansiSeq.ReplaceAllString(withPlan(t, 100, 40, 5).rawView(), "")
	if !strings.Contains(out, "Plan  ") {
		t.Fatalf("the panel is missing for a five-step plan:\n%s", out)
	}
	if strings.Contains(out, "more rows") {
		t.Errorf("a plan that fits is marked as cut:\n%s", out)
	}
	// Every step is on screen. Counting occurrences would be wrong: the header echoes the
	// in-progress step by design, so five steps legitimately produce six matches.
	for i := 0; i < 5; i++ {
		if !strings.Contains(out, fmt.Sprintf("step %02d", i)) {
			t.Errorf("step %02d is missing from a plan that fits:\n%s", i, out)
		}
	}
}

// Clipping must not cost the frame its bounds — the panel is composited into it, and a box that
// grew instead of shrinking would push the whole screen.
func TestTheClippedPanelStillFitsTheTerminal(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{100, 24, 25}, {100, 40, 40}, {140, 50, 25}} {
		s := withPlan(t, c.w, c.h, c.n)
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

// An unmeasured terminal is not a short one, and a panel that fits is returned as itself rather
// than rebuilt.
func TestTheRowClipLeavesWhatItDoesNotNeedToCut(t *testing.T) {
	rows := []string{"a", "b", "c"}
	if got := clipPanelRows(rows, 0); len(got) != 3 {
		t.Errorf("an unmeasured terminal clipped to %d rows", len(got))
	}
	if got := clipPanelRows(rows, 60); len(got) != 3 {
		t.Errorf("three rows on a 60-row terminal clipped to %d", len(got))
	}
	// …and a genuinely short one keeps the marker inside the room it was given.
	long := make([]string, 80)
	for i := range long {
		long[i] = fmt.Sprintf("row %d", i)
	}
	got := clipPanelRows(long, 30)
	if len(got) >= len(long) {
		t.Fatalf("80 rows survived a 30-row terminal (%d)", len(got))
	}
	if !strings.Contains(got[len(got)-1], "more rows") {
		t.Errorf("the last kept row is not the marker: %q", got[len(got)-1])
	}
}
