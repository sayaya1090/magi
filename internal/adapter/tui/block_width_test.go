package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/event"
)

// Every block kind, at four widths, against long content. One over-wide row is not confined to
// itself: the frame is a vertical join, so it pads every other row out to match, and the whole
// transcript starts wrapping in the terminal instead of in the renderer.
//
// Measured before the fixes: an error line was 352 cells on a long message, and the collapsed
// thought line 49 at width 40. A verdict row for a member like Balthasar is 43 cells at full
// detail, which a 40-column window cannot hold either.
//
// blockAssistant is left out on purpose: it renders through glamour, which wraps to the width it
// was built with, and a test Model has no glamour — the fallback path here is not the one a user
// sees. Its wrapping is covered where the renderer is actually configured.
func TestEveryBlockKindFitsTheTerminal(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	long := strings.Repeat("the linker could not find libfoo and then some more words ", 6)

	kinds := map[string]block{
		"toolCall/council":   {kind: blockToolCall, name: "council", args: `{"question":"` + long + `"}`, done: true, ok: true, result: `"` + long + `"`},
		"toolCall/read":      {kind: blockToolCall, name: "read", args: `{"path":"` + strings.Repeat("dir/", 40) + `f.go"}`, done: true, ok: true, result: `"` + long + `"`},
		"toolCall/websearch": {kind: blockToolCall, name: "websearch", args: `{"query":"` + long + `"}`, done: true, ok: true, result: `"` + long + `"`},
		"toolResult":         {kind: blockToolResult, ok: true, text: long},
		"error":              {kind: blockError, text: long},
		"info":               {kind: blockInfo, text: long},
		"reasoning":          {kind: blockReasoning, text: long},
		"user":               {kind: blockUser, text: long},
		// One member per block: applyEvent appends a block as each verdict lands and never
		// redraws, so this is the only shape the renderer is ever handed.
		"councilVerdict": {kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
			{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.72},
		}},
	}
	for _, w := range []int{40, 60, 80, 100} {
		m.width = w
		for name, blk := range kinds {
			for i, line := range strings.Split(m.renderBlock(blk), "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Errorf("width=%-3d %-19s line %d = %d cells: %q", w, name, i, lw, ansi.Strip(line))
				}
			}
		}
	}
}

// The verdict row is a summary; the GUIDANCE under it is the part a reader acts on — each rejecting
// member's feedback, and every member's keep. It has to survive the row's own layout changes and
// still fit.
//
// Measured before the fix: a keep line was 41 cells in a 40-column window, because the hanging
// indent under a long member name plus the 20-cell floor on the text together exceeded the window.
func TestCouncilGuidanceSurvivesAndFits(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	long := strings.Repeat("the acceptance check never ran so the claim is unproven ", 4)
	// One member per block, as applyEvent builds them.
	blocks := []block{
		{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
			{Member: "Melchior", Lens: "correctness", Decision: "done", Confidence: 0.9, Keep: long}}},
		{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
			{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.7, Feedback: long}}},
		{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
			{Member: "Casper", Lens: "completeness", Decision: "continue", Confidence: 0.6, Rationale: long}}},
	}
	for _, w := range []int{40, 60, 80, 120} {
		m.width = w
		var rows []string
		for _, b := range blocks {
			rows = append(rows, m.renderBlock(b))
		}
		out := strings.Join(rows, "\n")
		plain := ansi.Strip(out)
		// Present at all: a rejecting member's reason, and an approving member's keep.
		for _, want := range []string{"→ Balthasar:", "→ Casper:", "⊙ Melchior keep:"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width=%d: %q is missing from the guidance:\n%s", w, want, plain)
			}
		}
		if !strings.Contains(plain, "acceptance check never ran") {
			t.Errorf("width=%d: the guidance text itself is gone", w)
		}
		for i, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width=%-3d guidance line %d = %d cells: %q", w, i, lw, ansi.Strip(line))
			}
		}
		// Fitting by being CUT is not fitting. The guidance is wrapped, and the ellipsis backstop
		// exists for rows that cannot be wrapped — a guidance line reaching it means the wrap width
		// was computed too small, which is what the hanging indent used to do in a narrow window.
		if strings.Contains(plain, "…") {
			t.Errorf("width=%d: the guidance was truncated instead of wrapped:\n%s", w, plain)
		}
		// And it is all still there: wrapping must not drop words.
		words := strings.Fields(strings.ReplaceAll(plain, "\n", " "))
		if n := strings.Count(strings.Join(words, " "), "acceptance"); n < 12 {
			t.Errorf("width=%d: only %d of the 12 repeats survived the wrap:\n%s", w, n, plain)
		}
	}
}

// A verdict row holds ONE member and is never redrawn, so what has to fit is one segment:
// `● Balthasar  [verification]  ✗ reject · 72%` is 43 cells. Rather than cut it, the row gives up
// detail in the order it can least justify keeping — the confidence reading first, then the lens —
// and only clips if even the name and the verdict do not fit.
func TestAVerdictRowGivesUpDetailBeforeItIsCut(t *testing.T) {
	v := event.CouncilVerdictData{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.72}
	blk := block{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{v}}
	mm := newTestModel(t)
	m := &mm

	for _, c := range []struct {
		width    int
		wantLens bool
		wantConf bool
	}{
		{100, true, true},  // everything fits
		{44, true, false},  // the lens fits, the confidence does not
		{30, false, false}, // neither does
	} {
		m.width = c.width
		plain := ansi.Strip(m.renderBlock(blk))
		if got := strings.Contains(plain, "[verification]"); got != c.wantLens {
			t.Errorf("width=%d: lens shown=%v want %v: %q", c.width, got, c.wantLens, plain)
		}
		if got := strings.Contains(plain, "72%"); got != c.wantConf {
			t.Errorf("width=%d: confidence shown=%v want %v: %q", c.width, got, c.wantConf, plain)
		}
		// Who voted and how they voted is what the line is for, and neither is ever dropped.
		for _, want := range []string{"Balthasar", "reject"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width=%d: %q went missing: %q", c.width, want, plain)
			}
		}
		// Degrading, not cutting: the ellipsis is the last resort, not the mechanism.
		if c.width >= 30 && strings.Contains(plain, "…") {
			t.Errorf("width=%d: the row was cut when it could have shed detail: %q", c.width, plain)
		}
	}
}

// The click column is measured against the SAME detail level the row was drawn at. Measuring the
// full form against a degraded row puts the boundaries in the wrong columns.
func TestAClickOnADegradedRowStillOpensItsMember(t *testing.T) {
	v := event.CouncilVerdictData{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.72}
	mm := newTestModel(t)
	m := &mm
	m.blocks = []block{{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{v}}}
	m.blockLineStart = []int{0}
	for _, w := range []int{30, 44, 100} {
		m.width = w
		for _, col := range []int{2, 5, w - 1} {
			m.selAC = col
			if !m.openCouncilDetailAt(0) {
				t.Fatalf("width=%d col=%d: no detail opened", w, col)
			}
			if m.councilDetail.Member != "Balthasar" {
				t.Errorf("width=%d col=%d: opened %q", w, col, m.councilDetail.Member)
			}
		}
	}
}
