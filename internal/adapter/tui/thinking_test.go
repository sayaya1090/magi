package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A double-click (two rapid toggles on the same line) must end EXPANDED, not
// revert to collapsed — the 2nd toggle of the pair is swallowed by the debounce.
func TestThoughtDoubleClickExpands(t *testing.T) {
	applyTheme(true)
	m := &Model{roleColor: map[string]int{}, width: 100}
	m.blocks = []block{{kind: blockReasoning, text: "padding\nDETAIL"}}
	_ = m.transcript()
	line := m.blockLineStart[0]
	m.toggleThoughtAt(line) // 1st click → expand
	m.toggleThoughtAt(line) // 2nd click of a double-click → swallowed
	if !m.blocks[0].expanded {
		t.Error("double-click should leave the thought expanded, not reverted")
	}
}

// Expanded "thinking" text must wrap to the transcript width (terminal minus the
// side panel), not overflow it. Regression: reasoning was rendered raw, so long
// lines blew past a narrow/panel-shrunk transcript.
func TestThinkingWrapsToWidth(t *testing.T) {
	applyTheme(true)
	long := strings.Repeat("reasoning ", 30) // ~300 cols, one logical line
	for _, w := range []int{40, 80, 120} {
		m := &Model{roleColor: map[string]int{}, width: w}
		blk := block{kind: blockReasoning, text: long, expanded: true}
		out := m.renderBlock(blk)
		limit := m.transcriptWidth()
		for _, line := range strings.Split(out, "\n") {
			if vis := ansi.StringWidth(line); vis > limit {
				t.Errorf("width=%d: wrapped line %d cols exceeds transcript width %d: %q", w, vis, limit, ansi.Strip(line))
			}
		}
	}
}

// Reasoning blocks are collapsed to a dim one-liner by default and expanded with
// the toggle — matching how other agents fold "thinking".
func TestThinkingCollapseExpand(t *testing.T) {
	applyTheme(true)
	m := &Model{roleColor: map[string]int{}, width: 100}
	// A long thought whose detail sits beyond the collapsed preview's cutoff.
	detail := "SECRET-DETAIL-LINE"
	blk := block{kind: blockReasoning, text: strings.Repeat("padding ", 12) + "\n" + detail + "\nmore"}

	m.showThink = false
	collapsed := m.renderBlock(blk)
	if strings.Contains(collapsed, detail) {
		t.Errorf("collapsed reasoning should hide later detail: %q", collapsed)
	}
	if !strings.Contains(collapsed, "ctrl+t") {
		t.Errorf("collapsed reasoning should hint the toggle: %q", collapsed)
	}
	if strings.Count(collapsed, "\n") != 0 {
		t.Errorf("collapsed reasoning should be a single line: %q", collapsed)
	}

	m.showThink = true
	expanded := m.renderBlock(blk)
	if !strings.Contains(expanded, detail) {
		t.Errorf("expanded reasoning should show full text: %q", expanded)
	}
}

// Clicking a collapsed thought expands just that block (not all), and clicking
// again collapses it; clicking a non-reasoning block does nothing.
func TestThinkingClickToExpand(t *testing.T) {
	applyTheme(true)
	m := &Model{roleColor: map[string]int{}, width: 100}
	detail := "SECRET-DETAIL-LINE"
	m.blocks = []block{
		{kind: blockUser, text: "do the thing"},
		{kind: blockReasoning, text: strings.Repeat("padding ", 12) + "\n" + detail},
	}
	_ = m.transcript() // populates blockLineStart

	line := m.blockLineStart[1]
	if !m.toggleThoughtAt(line) {
		t.Fatal("clicking a reasoning block should toggle it")
	}
	if !m.blocks[1].expanded {
		t.Error("reasoning block should be expanded after click")
	}
	if m.showThink {
		t.Error("per-block click must not flip the global toggle")
	}
	_ = m.transcript()
	if got := m.renderBlock(m.blocks[1]); !strings.Contains(got, detail) {
		t.Errorf("expanded-by-click block should show detail: %q", got)
	}
	// A deliberate (well-separated) second click collapses. Reset the debounce
	// clock so this isn't mistaken for the 2nd half of a double-click.
	m.lastThoughtAt = time.Time{}
	m.toggleThoughtAt(m.blockLineStart[1])
	if m.blocks[1].expanded {
		t.Error("second click should collapse the block")
	}
	// Clicking the user block does nothing.
	if m.toggleThoughtAt(m.blockLineStart[0]) {
		t.Error("clicking a non-reasoning block should not toggle")
	}
}
