package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
)

// buildInput mirrors the textarea configured in newModel (model.go:234-243).
func buildInput(width int) textarea.Model {
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.SetWidth(width)
	ta.Focus()
	return ta
}

// visibleRows counts the non-empty content rows the textarea actually renders,
// stripping the prompt so we measure how much of the input is on screen.
func visibleRows(v string) int {
	n := 0
	for _, ln := range strings.Split(v, "\n") {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "❯")) != "" {
			n++
		}
	}
	return n
}

// PATHOLOGY: a long single-line prompt (no explicit newline) soft-wraps to many
// display rows, but LineCount() returns 1 (it counts logical lines, not wrapped
// rows). refresh() grows the box via clampInt(LineCount(),1,maxInputRows), so the
// box stays at height 1 and only the last wrapped row is visible — "여러 줄일 때
// 다 안 보여". This test pins the current wrong behavior; the DynamicHeight test
// below shows the fix.
func TestSoftWrappedInputHiddenByLineCount(t *testing.T) {
	const width = 40
	ta := buildInput(width)
	// ~5 wrapped rows worth of text on a single logical line.
	ta.SetValue(strings.Repeat("wrap ", 40))

	if got := ta.LineCount(); got != 1 {
		t.Fatalf("precondition: a newline-free string is 1 logical line, LineCount()=%d", got)
	}
	// This is exactly what refresh() computes today.
	rows := clampInt(ta.LineCount(), 1, maxInputRows)
	ta.SetHeight(rows)
	if rows != 1 {
		t.Fatalf("LineCount-driven height should stay 1 for a wrapped single line, got %d", rows)
	}
	if vr := visibleRows(ta.View()); vr > 1 {
		t.Fatalf("BUG NOT REPRODUCED: expected the wrapped input clipped to 1 row, saw %d rows", vr)
	}
	t.Logf("REPRODUCED: %d-column single line wraps but box height stays %d row (rest hidden)", width, rows)
}

// FIX DIRECTION: DynamicHeight sizes the box from totalVisualLines() (soft-wrap
// aware), bounded by MinHeight/MaxHeight, so the same wrapped input grows to
// several rows instead of 1.
func TestDynamicHeightShowsWrappedInput(t *testing.T) {
	const width = 40
	ta := buildInput(width)
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = maxInputRows
	ta.SetValue(strings.Repeat("wrap ", 40))
	// Re-trigger the internal recalculation the same way refresh() would (width set).
	ta.SetWidth(width)

	if ta.Height() <= 1 {
		t.Fatalf("DynamicHeight should grow the box past 1 row for wrapped input, got %d", ta.Height())
	}
	if ta.Height() > maxInputRows {
		t.Fatalf("DynamicHeight must clamp at MaxHeight=%d, got %d", maxInputRows, ta.Height())
	}
	if vr := visibleRows(ta.View()); vr <= 1 {
		t.Fatalf("expected multiple visible rows with DynamicHeight, saw %d", vr)
	}
	t.Logf("FIXED: DynamicHeight grew box to %d rows for the same wrapped input", ta.Height())
}
