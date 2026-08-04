package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Each plan status renders its own glyph; a cancelled (aborted/incomplete) todo
// shows ✗ so the post-it reflects what was left undone.
func TestTodoLineGlyphs(t *testing.T) {
	cases := map[string]string{"completed": "✓", "in_progress": "◐", "pending": "☐", "cancelled": "✗"}
	for status, glyph := range cases {
		if got := todoLine(session.Todo{Content: "task", Status: status}, 40, 0); !strings.Contains(got, glyph) {
			t.Errorf("status %q should render %q, got %q", status, glyph, got)
		}
	}
}

// A nested plan node (a child session's todo under the parent step) is indented two
// spaces per depth; depth 0 has no leading indent. This is how the tree structure
// reads in the post-it.
func TestTodoLineDepthIndent(t *testing.T) {
	td := session.Todo{Content: "sub task", Status: "pending"}
	if got := todoLine(td, 40, 0); strings.HasPrefix(got, " ") {
		t.Errorf("depth 0 should not be indented, got %q", got)
	}
	got1 := todoLine(td, 40, 1)
	if !strings.HasPrefix(got1, "  ") {
		t.Errorf("depth 1 should start with 2-space indent, got %q", got1)
	}
	if got2 := todoLine(td, 40, 2); !strings.HasPrefix(got2, "    ") {
		t.Errorf("depth 2 should start with 4-space indent, got %q", got2)
	}
}

// The post-it's left edge is draggable to resize its width.
func TestPanelResizeEdge(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 100, 40
	m.panes = []*agentPane{{role: "coder"}} // makes hasPanel true
	_, top, left, ok := m.floatPanel()
	if !ok {
		t.Fatal("post-it should be shown")
	}
	if !m.onPanelSplitter(left, top) {
		t.Fatalf("the box's left edge (col %d) should be the resize handle", left)
	}
	if m.onPanelSplitter(left-20, top) {
		t.Fatal("a column well left of the box is not the handle")
	}
	// Drag the edge left → wider box (right edge fixed at width-floatMarginRight).
	before := m.panelW
	m.setPanelWidthForSplit(left - 10)
	if m.panelW <= before {
		t.Fatalf("dragging the edge left should widen the panel: %d -> %d", before, m.panelW)
	}
	// Clamp to the minimum width.
	m.setPanelWidthForSplit(m.width - 1)
	if m.panelW != 24 {
		t.Fatalf("min width clamp = %d, want 24", m.panelW)
	}
}

// Clicking a subagent row in the floating post-it opens that subagent's detail view
// (focus + zoom), like clicking its pane. Drives the real render path so the hit-test
// Y is recorded on the shared *agentPane (View has a value receiver, so it must
// persist via the pointer, not a Model value field).
func TestHandlePanelClick(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height, m.ready = 100, 40, true
	m.vp.SetWidth(100)
	m.vp.SetHeight(20)
	m.panes = []*agentPane{{role: "coder"}, {role: "tester"}}

	_ = m.View() // records each subagent's absolute panelY on the *agentPane
	box, top, left, ok := m.floatPanel()
	if !ok {
		t.Fatal("the post-it should be shown when there are subagents")
	}
	_ = box
	if m.panes[0].panelY <= 0 || m.panes[1].panelY <= 0 {
		t.Fatalf("subagent rows not recorded via View: %d, %d", m.panes[0].panelY, m.panes[1].panelY)
	}
	if m.panes[1].panelY == m.panes[0].panelY {
		t.Fatal("the two subagent rows should be on different lines")
	}
	inBoxX := left + 2 // a column inside the post-it box

	// Click the 2nd subagent → zoom into it.
	if !m.handlePanelClick(inBoxX, m.panes[1].panelY) {
		t.Fatal("click on a subagent row should be consumed")
	}
	if m.focusPane != 1 || !m.zoom {
		t.Fatalf("expected focus=1 zoom=true, got focus=%d zoom=%v", m.focusPane, m.zoom)
	}

	// A click outside the box (far left, in the transcript) is ignored.
	m.focusPane, m.zoom = -1, false
	if m.handlePanelClick(2, m.panes[0].panelY) {
		t.Fatal("click outside the post-it should not be consumed")
	}
	// A click inside the box but not on a subagent row (the top border row) is CONSUMED —
	// so it doesn't fall through to the transcript — but changes no focus.
	m.focusPane, m.zoom = -1, false
	if !m.handlePanelClick(inBoxX, top) {
		t.Fatal("click on empty post-it area should be consumed (not fall through)")
	}
	if m.focusPane != -1 || m.zoom {
		t.Fatalf("empty-area click should not change focus/zoom, got focus=%d zoom=%v", m.focusPane, m.zoom)
	}
}

// The record section is ONE line of counts.
//
// It used to list all four categories in full and unbounded, and the longest was the least
// useful: the commands that WORKED. Since clipPanelRows bounds the whole panel to the screen,
// that list pushed Plan, Background and Context off the bottom. The failures kept a row longest
// and lost it too: a command is wider than this column, so the row showed a truncated head with
// the part naming the failure cut off. The full record still reaches the model every step
// through runState, and the transcript shows each command with its exit as it runs.
func TestObservedIsOneLineOfCounts(t *testing.T) {
	clean := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		clean = append(clean, fmt.Sprintf("grep -n thing%02d src.go", i))
	}
	obs := app.Observation{
		Changed: []string{"a.go", "b.go", "c.go"},
		Clean:   clean,
		Failed:  []string{"go build ./... (exit 2)"},
		Unknown: []string{"cat x | head"},
	}
	rows := observedRows(obs, 60)
	if len(rows) != 1 {
		t.Fatalf("the record is one line, got %d:\n%s", len(rows), ansi.Strip(strings.Join(rows, "\n")))
	}
	line := ansi.Strip(rows[0])
	for _, want := range []string{"±3", "✓20", "✗1", "?1"} {
		if !strings.Contains(line, want) {
			t.Errorf("counts line missing %q: %q", want, line)
		}
	}
	// No command text at all — not the clean ones, and not the failed one either.
	for _, cmd := range append(append([]string{}, clean...), "go build", "cat x") {
		if strings.Contains(line, cmd) {
			t.Errorf("a command reached the panel (%q): %q", cmd, line)
		}
	}
}

// An empty category contributes no glyph, so a turn that only read files does not display "✗0".
func TestObservedOmitsEmptyCategories(t *testing.T) {
	rows := observedRows(app.Observation{Clean: []string{"ls"}}, 60)
	line := ansi.Strip(rows[0])
	if !strings.Contains(line, "✓1") {
		t.Errorf("the one category present must show: %q", line)
	}
	for _, no := range []string{"±", "✗", "?"} {
		if strings.Contains(line, no) {
			t.Errorf("empty category %q rendered anyway: %q", no, line)
		}
	}
}
