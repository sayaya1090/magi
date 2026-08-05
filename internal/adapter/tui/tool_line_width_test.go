package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A tool line is a one-line summary, and it has to be one line of the terminal the user actually
// has. Its two variable parts were capped at fixed sizes — 80 cells for the argument preview, 120
// for the result — with no idea how wide the window was, so a bash call with a long command and a
// long first output line rendered the same 200-odd cells at every width and simply ran off the
// edge. In a vertically joined frame one over-wide row also pads every other row out to match it.
//
// Measured before the fix: 202 cells at width 60 and again at width 100.
func TestAToolLineFitsTheTerminal(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	long := strings.Repeat("echo hello world; ", 12)
	out := strings.Repeat("output line ", 20)

	for _, w := range []int{40, 60, 80, 100, 200} {
		m.width = w
		for _, blk := range []block{
			// A finished call with both halves long: they have to share the room.
			{kind: blockToolCall, name: "bash", args: `{"command":"` + long + `"}`,
				done: true, ok: true, result: `"` + out + `"`},
			// Still running: no result yet, so the args may use what the summary would have.
			{kind: blockToolCall, name: "bash", args: `{"command":"` + long + `"}`},
			// A failing call — the glyph changes, the budget must not.
			{kind: blockToolCall, name: "bash", args: `{"command":"` + long + `"}`,
				done: true, ok: false, result: `"` + out + `"`},
			// A result with no matching call renders through the fallback line.
			{kind: blockToolResult, ok: true, text: out},
		} {
			for i, line := range strings.Split(m.renderBlock(blk), "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Errorf("width=%d %s line %d is %d cells: %q",
						w, blk.name, i, lw, ansi.Strip(line))
				}
			}
		}
	}
}

// The outcome must not be squeezed out by a long command. Both halves are wanted, so when they do
// not both fit each gets half and whatever one does not want goes to the other.
func TestALongCommandStillLeavesRoomForTheOutcome(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 100
	blk := block{kind: blockToolCall, name: "bash",
		args: `{"command":"` + strings.Repeat("echo hello world; ", 12) + `"}`,
		done: true, ok: true, result: `"BUILD FAILED: the linker could not find libfoo"`}
	head := ansi.Strip(strings.Split(m.renderBlock(blk), "\n")[0])
	if !strings.Contains(head, "⟶") {
		t.Fatalf("the result was pushed off the line entirely:\n%s", head)
	}
	if !strings.Contains(head, "BUILD FAILED") {
		t.Errorf("the outcome should survive a long command, at least in part:\n%s", head)
	}
	if !strings.Contains(head, "echo hello world") {
		t.Errorf("the command should survive too, at least in part:\n%s", head)
	}
}

// splitHeadRoom's own contract: never over budget, and an unwanted half is given away.
func TestSplitHeadRoom(t *testing.T) {
	for _, c := range []struct{ room, aWant, sWant, a, s int }{
		{100, 30, 20, 30, 20},  // both fit
		{40, 100, 100, 20, 20}, // neither fits: half each
		{40, 5, 100, 5, 35},    // args wants little: the rest goes to the summary
		{40, 100, 5, 35, 5},    // and the other way round
		{0, 50, 50, 0, 0},      // no room at all
		{-3, 50, 50, 0, 0},     // never negative
	} {
		a, s := splitHeadRoom(c.room, c.aWant, c.sWant)
		if a != c.a || s != c.s {
			t.Errorf("splitHeadRoom(%d,%d,%d) = %d,%d want %d,%d", c.room, c.aWant, c.sWant, a, s, c.a, c.s)
		}
		if c.room > 0 && a+s > c.room {
			t.Errorf("splitHeadRoom(%d,%d,%d) handed out %d of %d", c.room, c.aWant, c.sWant, a+s, c.room)
		}
	}
}
