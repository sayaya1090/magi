package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
)

// The scroll-position chip replaces the drawn scrollbar: hidden when the
// content fits, shows bottom-line/total (with percent) when scrollable, and
// flags fresh output arriving below when the user scrolled up mid-stream.
func TestScrollMeter(t *testing.T) {
	applyTheme(true)
	m := &Model{}
	m.vp = viewport.New()
	m.vp.SetWidth(20)
	m.vp.SetHeight(5)

	// Fits → no chip.
	m.contentPlain = []string{"a", "b", "c"}
	m.vp.SetContent("a\nb\nc")
	if got := m.scrollMeter(); got != "" {
		t.Errorf("content that fits should render no chip, got %q", stripANSI(got))
	}

	// Overflow, at bottom → 100% and total/total, no "new" marker even while running.
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "x"
	}
	m.contentPlain = lines
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.vp.GotoBottom()
	m.running = true
	got := stripANSI(m.scrollMeter())
	if !strings.Contains(got, "100%") || !strings.Contains(got, "(20/20)") {
		t.Errorf("at bottom want 100%% (20/20), got %q", got)
	}
	if strings.Contains(got, "new") {
		t.Errorf("at bottom there is no unseen output, got %q", got)
	}

	// Scrolled up while streaming → position + the new-output marker.
	m.vp.GotoTop()
	got = stripANSI(m.scrollMeter())
	if !strings.Contains(got, "(5/20)") || !strings.Contains(got, "↓ new") {
		t.Errorf("scrolled up mid-stream should show position and ↓ new, got %q", got)
	}

	// Scrolled up but idle → position only.
	m.running = false
	if got := stripANSI(m.scrollMeter()); strings.Contains(got, "new") {
		t.Errorf("idle session should not flag new output, got %q", got)
	}
}
