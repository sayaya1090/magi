package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
)

func TestRenderScrollbar(t *testing.T) {
	applyTheme(true)
	m := &Model{}
	m.vp = viewport.New()
	m.vp.SetWidth(20)
	m.vp.SetHeight(5)

	// Content that fits the viewport → a blank gutter (no thumb), still `height` rows.
	m.contentPlain = []string{"a", "b", "c"}
	m.vp.SetContent("a\nb\nc")
	out := m.renderScrollbar(5)
	if strings.TrimSpace(stripANSI(out)) != "" {
		t.Errorf("content that fits should give a blank gutter, got %q", stripANSI(out))
	}
	if n := len(strings.Split(out, "\n")); n != 5 {
		t.Errorf("scrollbar should always be height rows, got %d", n)
	}

	// Overflow → a thumb, at the top when scrolled to the top.
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "x"
	}
	m.contentPlain = lines
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.vp.GotoTop()
	top := strings.Split(stripANSI(m.renderScrollbar(5)), "\n")
	if !strings.Contains(strings.Join(top, ""), "█") {
		t.Errorf("overflow should render a thumb: %v", top)
	}
	if !strings.Contains(top[0], "█") {
		t.Errorf("at top, the thumb should be at the first row: %v", top)
	}

	// Scrolled to the bottom → the thumb reaches the last row.
	m.vp.GotoBottom()
	bot := strings.Split(stripANSI(m.renderScrollbar(5)), "\n")
	if !strings.Contains(bot[len(bot)-1], "█") {
		t.Errorf("at bottom, the thumb should reach the last row: %v", bot)
	}
	if strings.Contains(bot[0], "█") {
		t.Errorf("at bottom, the thumb should have left the first row: %v", bot)
	}
}
