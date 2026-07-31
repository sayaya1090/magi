package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// jobPane builds the pane a background job opens, with the fields syncJobPanes fills in. A long
// command is the ordinary case, not a contrived one — the strip follows `bash -c '…'` lines.
func jobPane() *agentPane {
	return &agentPane{
		job: "bg_1", role: "bg_1", sub: 1, started: time.Now().Add(-90 * time.Second),
		task: "bash -c 'cd /app && make -j8 CFLAGS=-O2 all && ./run_integration_suite --verbose'",
		live: "compiling module 42 of 97\n",
	}
}

// After the turn ends, a still-running background job collapses to one row per pane. That row is
// assembled from four parts and only one of them was bounded — desc, against width-8, while the
// status and the focus hint that follow it run past forty cells together. The row drew 82 cells in
// an 80-column terminal and 79 in a 40-column one.
//
// The check is on the WHOLE frame, not the strip function: the frame is joined vertically, so one
// over-wide row pads every other row and the terminal wraps the entire screen.
func TestTheFinishedJobStripFitsItsTerminal(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{40, 60, 80, 100} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 30})
		s.m.running = false // the turn ended; the job did not
		s.m.subID++
		s.m.panes = append(s.m.panes, jobPane())
		s.m.focusPane = 0 // the focused pane is the one that carries the extra hint
		s.m.refresh()     // what the render tick does after syncJobPanes marks the model dirty

		var strip string
		for i, line := range strings.Split(s.rawView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("width %d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
			if strings.Contains(trimmed, "bg_1") {
				strip = trimmed
			}
		}
		if strip == "" {
			t.Fatalf("width %d: the job's strip row is not on screen, so nothing here was tested", w)
		}
		// What survives the narrowing is what the row is for: which job, and how it is doing.
		if !strings.Contains(strip, "bg_1") || !strings.Contains(strip, "running") {
			t.Errorf("width %d: the row lost the job or its status: %q", w, strip)
		}
	}
}

// The hint is what goes first when the row will not fit. It tells the user how to open the pane;
// the status tells them whether the build failed, and only one of those can be dropped.
func TestTheJobStripDropsTheHintBeforeTheStatus(t *testing.T) {
	applyTheme(true)
	row := func(w int) string {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 30})
		s.m.running = false
		s.m.subID++
		p := jobPane()
		p.done, p.exited, p.exit = true, true, 1 // a build that failed
		s.m.panes = append(s.m.panes, p)
		s.m.focusPane = 0
		s.m.refresh()
		for _, line := range strings.Split(s.rawView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if strings.Contains(trimmed, "bg_1") {
				return trimmed
			}
		}
		t.Fatalf("width %d: no strip row", w)
		return ""
	}
	narrow := row(40)
	if strings.Contains(narrow, "ctrl+o") {
		t.Errorf("the hint was kept at 40 cells, where the row cannot hold it: %q", narrow)
	}
	// A failed job says so at every width — this is the row that makes a broken build visible
	// without opening it.
	for _, w := range []int{40, 60, 80, 100} {
		if got := row(w); !strings.Contains(got, "exit 1") && !strings.Contains(got, "✗") {
			t.Errorf("width %d: a failed job renders with no sign of failure: %q", w, got)
		}
	}
}

// clipRow is the backstop under all of this: it marks what it cuts, leaves a fitting row alone,
// and treats an unmeasured terminal as unbounded rather than as zero-width.
func TestClipRowMarksWhatItCuts(t *testing.T) {
	const s = "a row that is definitely longer than twenty cells"
	got := clipRow(s, 20)
	if lipgloss.Width(got) > 20 {
		t.Errorf("clipped to %d cells: %q", lipgloss.Width(got), got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("an unmarked cut reads as the whole row: %q", got)
	}
	if got := clipRow("short", 20); got != "short" {
		t.Errorf("a fitting row was altered: %q", got)
	}
	if got := clipRow(s, 0); got != s {
		t.Errorf("width 0 must leave the row alone, got %q", got)
	}
}
