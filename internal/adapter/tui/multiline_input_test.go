package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
)

// newInputArea mirrors the input textarea configured in newModel (model.go): the
// box is sized from the true visual line count (soft wraps included) via
// DynamicHeight, capped at maxInputRows.
func newInputArea(width int) textarea.Model {
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = maxInputRows
	ta.SetWidth(width)
	ta.Focus()
	return ta
}

// A long single logical line (no explicit newline) soft-wraps to several display
// rows; the box must grow to show them instead of clipping to the last row.
// Regression for "여러 줄일 때 다 안 보여" — the old refresh() sized from
// LineCount(), which counts logical lines only and left the box at height 1.
func TestWrappedLongLineGrowsInputBox(t *testing.T) {
	ta := newInputArea(40)
	ta.SetValue(strings.Repeat("wrap ", 40)) // 1 logical line, many wrapped rows
	ta.SetWidth(40)                          // re-trigger DynamicHeight recalculation

	if ta.LineCount() != 1 {
		t.Fatalf("precondition: newline-free text is 1 logical line, got LineCount()=%d", ta.LineCount())
	}
	if ta.Height() <= 1 {
		t.Fatalf("wrapped long line should grow the box past 1 row, got Height()=%d", ta.Height())
	}
	if ta.Height() > maxInputRows {
		t.Fatalf("box must stay capped at maxInputRows=%d, got Height()=%d", maxInputRows, ta.Height())
	}
}

// Inserting explicit newlines (the alt+enter / ctrl+j path) grows the box row by
// row up to the cap.
func TestNewlineInsertionGrowsInputBox(t *testing.T) {
	ta := newInputArea(60)
	if ta.Height() != 1 {
		t.Fatalf("empty input should start at 1 row, got Height()=%d", ta.Height())
	}
	ta.InsertString("first")
	ta.InsertString("\n") // what the alt+enter/ctrl+j key case does
	ta.InsertString("second")
	if ta.LineCount() != 2 {
		t.Fatalf("two logical lines expected after one newline, got LineCount()=%d", ta.LineCount())
	}
	if ta.Height() < 2 {
		t.Fatalf("box should grow to at least 2 rows for two lines, got Height()=%d", ta.Height())
	}

	// Past the cap, height clamps at maxInputRows.
	for i := 0; i < maxInputRows+3; i++ {
		ta.InsertString("\nx")
	}
	if ta.Height() != maxInputRows {
		t.Fatalf("many lines should clamp the box at maxInputRows=%d, got Height()=%d", maxInputRows, ta.Height())
	}
}
