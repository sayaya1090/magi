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
// Measured before the fixes: a council verdict row was 131 cells at every width (all three members
// on one line, unbounded), an error line 352, and the collapsed thought line 49 at width 40.
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
		"councilVerdict": {kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
			{Member: "Melchior", Lens: "correctness", Decision: "done", Confidence: 0.91},
			{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.72},
			{Member: "Casper", Lens: "completeness", Decision: "abstain", Confidence: 0.5},
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

// The verdict row is clickable, and the click column alone identified the member — which is only
// right while they share one line. Now that the row wraps, the renderer and the hit-test must lay
// the members out the same way or a click on the second row opens a member from the first.
func TestClickingAWrappedVerdictRowPicksTheMemberUnderIt(t *testing.T) {
	vs := []event.CouncilVerdictData{
		{Member: "Melchior", Lens: "correctness", Decision: "done", Confidence: 0.91},
		{Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.72},
		{Member: "Casper", Lens: "completeness", Decision: "abstain", Confidence: 0.5},
	}
	mm := newTestModel(t)
	m := &mm
	m.width = 50 // narrow enough that the three do not share a line
	m.blocks = []block{{kind: blockCouncilVerdict, councilVerdicts: vs}}
	m.blockLineStart = []int{0}

	starts := councilRowPack(vs, m.bodyWidth()-2)
	if len(starts) < 2 {
		t.Fatalf("width 50 should wrap the trio, got %d row(s)", len(starts))
	}
	// The rendered row count must equal what the hit-test packs against.
	if got := len(strings.Split(m.renderBlock(m.blocks[0]), "\n")); got != len(starts) {
		t.Fatalf("renderer drew %d rows, hit-test packs %d — they have drifted", got, len(starts))
	}
	// A click at the left edge of each row opens the first member ON THAT ROW.
	for row, first := range starts {
		m.selAC = 2 // the indent, i.e. the first member's opening column
		if !m.openCouncilDetailAt(row) {
			t.Fatalf("row %d: no detail opened", row)
		}
		if m.councilDetail.Member != vs[first].Member {
			t.Errorf("clicking row %d opened %q, want %q", row, m.councilDetail.Member, vs[first].Member)
		}
	}
}

// Packing never splits a member and never over-fills a row.
func TestCouncilRowPack(t *testing.T) {
	vs := []event.CouncilVerdictData{
		{Member: "Melchior", Lens: "correctness", Decision: "done"},
		{Member: "Balthasar", Lens: "verification", Decision: "continue"},
		{Member: "Casper", Lens: "completeness", Decision: "abstain"},
	}
	if got := councilRowPack(nil, 80); got != nil {
		t.Errorf("no members packs to nothing, got %v", got)
	}
	if got := councilRowPack(vs, 10000); len(got) != 1 {
		t.Errorf("a wide window keeps them on one row, got %d", len(got))
	}
	if got := councilRowPack(vs, 0); len(got) != 1 {
		t.Errorf("an unknown width must not fan them out, got %d rows", len(got))
	}
	sep := ansi.StringWidth(councilRowSep)
	for _, w := range []int{20, 40, 60, 80, 120} {
		starts := councilRowPack(vs, w)
		for k, from := range starts {
			to := len(vs)
			if k+1 < len(starts) {
				to = starts[k+1]
			}
			used := 0
			for i := from; i < to; i++ {
				if i > from {
					used += sep
				}
				used += ansi.StringWidth(councilMemberPlain(vs[i]))
			}
			// A single member wider than the window is the one case packing cannot fix; the
			// renderer clips that row instead.
			if to-from > 1 && used > w {
				t.Errorf("width=%d row %d holds %d cells", w, k, used)
			}
		}
	}
}
