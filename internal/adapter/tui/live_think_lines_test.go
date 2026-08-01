package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A message typed while the model is THINKING renders below the live section, and its
// recorded start line is what turns a click into that block. The live-thinking branch
// wrote its rows into the frame without adding them to the running line count, so every
// block emitted after it was recorded a few lines too high: the counter still ascended,
// so the ordering invariant passed, and the click landed on the wrong bubble.
//
// Both foldings are checked — collapsed under-counted by one line, expanded by the whole
// thought.
func TestQueuedBubbleKeepsItsLineWhileThinking(t *testing.T) {
	for _, tc := range []struct {
		name  string
		show  bool
		think string
	}{
		{"collapsed", false, "weighing the two approaches"},
		{"expanded", true, "weighing the two approaches\nand a second line\nand a third"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mm := newTestModel(t)
			m := &mm
			m.width, m.height = 80, 30
			m.blocks = []block{
				{kind: blockUser, text: "first question", reqID: "r1"},
				{kind: blockUser, text: "typed while it thinks", reqID: "r2", queued: true},
			}
			m.running = true
			m.showThink = tc.show
			m.liveThink = tc.think

			content := m.transcript()
			lines := strings.Split(content, "\n")
			for i, l := range lines {
				lines[i] = ansi.Strip(l)
			}

			if len(m.blockLineStart) != len(m.blocks) {
				t.Fatalf("%d start lines for %d blocks", len(m.blockLineStart), len(m.blocks))
			}
			for i, start := range m.blockLineStart {
				if start < 0 || start >= len(lines) {
					t.Fatalf("block %d starts at line %d, outside %d lines", i, start, len(lines))
				}
				want := ansi.Strip(strings.SplitN(m.cache[i], "\n", 2)[0])
				if got := lines[start]; got != want {
					t.Errorf("block %d is recorded at line %d, which holds\n  %q\nbut the block starts with\n  %q\nframe:\n%s",
						i, start, got, want, strings.Join(lines, "\n"))
				}
			}
		})
	}
}
