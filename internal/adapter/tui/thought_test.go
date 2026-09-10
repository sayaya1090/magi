package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sayaya1090/magi/internal/core/event"
)

// A silent member's thinking shows WITHOUT being asked for; an answering member's hides with the
// rest of the reasoning.
//
// The gate differs on purpose. A member that answered has a rationale under it, so its thought is
// one more piece of thinking and belongs behind ctrl+t with all the others. A silent member has no
// rationale by definition — "a verdict nobody gave" — and hiding the thought there reproduces the
// exact reading this field was added to end: thousands of characters of work, drawn as a
// considered shrug.
func TestASilentMembersThinkingIsNotHiddenBehindCtrlT(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 100
	m.showThink = false

	silent := block{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
		{Member: "Balthasar", Lens: "verification", Decision: "abstain", Silent: true,
			Thought: "I cannot find the test run the report names"},
	}}
	out := ansi.Strip(m.renderBlock(silent))
	if !strings.Contains(out, "I cannot find the test run") {
		t.Errorf("답이 없는 멤버의 생각이 안 보인다 — 왜 답이 없었는지 화면에 아무것도 없다:\n%s", out)
	}

	spoke := block{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
		{Member: "Balthasar", Lens: "verification", Decision: "done", Confidence: 0.8,
			Rationale: "the suite covers it", Thought: "weighing the two readings"},
	}}
	out = ansi.Strip(m.renderBlock(spoke))
	if strings.Contains(out, "weighing the two readings") {
		t.Errorf("답한 멤버의 생각이 ctrl+t 없이 나온다 — 나머지 추론과 규칙이 다르다:\n%s", out)
	}
	m.showThink = true
	out = ansi.Strip(m.renderBlock(spoke))
	if !strings.Contains(out, "weighing the two readings") {
		t.Errorf("ctrl+t 를 켰는데도 생각이 안 나온다:\n%s", out)
	}
}

// And it wraps like everything else in this block: a thought is the longest thing here.
func TestAThoughtNeverOverflowsTheWindow(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	long := strings.Repeat("the linker could not find libfoo and then some more words ", 6)
	blk := block{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{
		{Member: "Balthasar", Lens: "verification", Decision: "abstain", Silent: true, Thought: long},
	}}
	for _, w := range []int{40, 60, 80, 100} {
		m.width = w
		for i, line := range strings.Split(m.renderBlock(blk), "\n") {
			if lw := ansi.StringWidth(line); lw > w {
				t.Errorf("width=%-3d 줄 %d = %d 칸: %q", w, i, lw, ansi.Strip(line))
			}
		}
	}
}
