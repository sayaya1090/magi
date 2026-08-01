package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

// withWorkerPlan puts a subagent pane on screen with an n-step plan of its own.
func withWorkerPlan(t *testing.T, w, h, n int) (*script, *agentPane) {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	s.assistantText("working")
	p := &agentPane{sid: s.m.sid, role: "coder", task: "do the work"}
	s.m.panes = append(s.m.panes, p)
	var td []session.Todo
	for i := 0; i < n; i++ {
		st := "pending"
		if i == 0 {
			st = "in_progress"
		}
		td = append(td, session.Todo{Content: fmt.Sprintf("step %02d of the worker plan", i), Status: st})
	}
	s.m.app.SetTodos(p.sid, td)
	s.m.refresh()
	return s, p
}

// The worker panel clips its rows to the room it has — the overview panel's own fix cites this one
// as already correct — but it ended mid-list in silence. A plan cut at the fourth step reads as a
// worker with four steps, which is the same unmarked cut, on the panel held up as the example.
func TestAClippedWorkerPanelSaysHowManyItDropped(t *testing.T) {
	applyTheme(true)
	s, p := withWorkerPlan(t, 100, 24, 40)
	out := ansiSeq.ReplaceAllString(s.m.workerPanel(p), "")
	if out == "" {
		t.Fatal("the panel drew nothing, so nothing here is under test")
	}
	if !strings.Contains(out, "more rows") {
		t.Errorf("a forty-step plan was cut with nothing saying so:\n%s", out)
	}
}

// A plan that fits keeps every row and carries no marker.
func TestAWorkerPanelThatFitsIsUntouched(t *testing.T) {
	applyTheme(true)
	s, p := withWorkerPlan(t, 100, 50, 3)
	out := ansiSeq.ReplaceAllString(s.m.workerPanel(p), "")
	if strings.Contains(out, "more rows") {
		t.Errorf("a three-step plan is marked as cut:\n%s", out)
	}
	if !strings.Contains(out, "Plan  0/3") {
		t.Errorf("the plan header is missing:\n%s", out)
	}
}

// Clipping must not cost the box its bounds: the marker replaces a row rather than adding one.
func TestTheClippedWorkerPanelStaysInsideItsRoom(t *testing.T) {
	applyTheme(true)
	for _, c := range []struct{ w, h, n int }{{100, 24, 40}, {100, 16, 25}, {120, 30, 60}, {100, 50, 3}} {
		s, p := withWorkerPlan(t, c.w, c.h, c.n)
		out := s.m.workerPanel(p)
		if out == "" {
			continue
		}
		if maxRows := c.h - floatMarginTop - headerRows - 6; maxRows > 4 {
			if got := lipgloss.Height(out); got > maxRows+2 { // +2 = the box's own border
				t.Errorf("w=%d h=%d n=%d: the panel is %d rows into %d of room",
					c.w, c.h, c.n, got, maxRows)
			}
		}
	}
}
