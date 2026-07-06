package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/sayaya1090/magi/internal/core/event"
)

// question.requested opens the selection modal; the view numbers the options
// and highlights the pick.
func TestQuestionModal(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 80
	m.applyEvent(ev(t, event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID: "c1#1", Question: "which approach?", Options: []string{"redis", "in-memory"},
	}))
	if m.quest == nil || m.quest.callID != "c1#1" {
		t.Fatalf("modal should open on question.requested: %+v", m.quest)
	}
	v := stripANSI(m.questView())
	if !strings.Contains(v, "which approach?") || !strings.Contains(v, "1. redis") || !strings.Contains(v, "2. in-memory") {
		t.Fatalf("modal missing question/options: %q", v)
	}
	if !strings.Contains(v, "› 1. redis") {
		t.Fatalf("first option should start selected: %q", v)
	}
	m.quest.sel = 1
	if v := stripANSI(m.questView()); !strings.Contains(v, "› 2. in-memory") {
		t.Fatalf("selection should move: %q", v)
	}
}

// The quest modal must be accounted for in the chrome height, or it pushes the
// input/footer below the screen (the viewport doesn't shrink to make room).
func TestQuestModalReservesHeight(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	base := m.baseChromeHeight()
	m.quest = &questReq{question: "q", options: []string{"a", "b"}}
	if got := m.baseChromeHeight(); got != base+6 {
		t.Fatalf("quest modal should reserve options+4 rows: base=%d got=%d", base, got)
	}
	m.quest = nil
	m.perm = &permReq{name: "bash", args: "{}", reason: "network egress command"}
	if got := m.baseChromeHeight(); got != base+7 {
		t.Fatalf("perm modal with a reason line should reserve 7 rows: base=%d got=%d", base, got)
	}
	m.perm.reason = ""
	if got := m.baseChromeHeight(); got != base+6 {
		t.Fatalf("plain perm modal reserves 6 rows: base=%d got=%d", base, got)
	}
}

// Edge case (subagent review): at narrow widths the modal's hint/question/reason
// lines wrap, so a hardcoded row count under-reserves and the input/footer fall
// off the bottom. The reserve must track the *rendered* height. We drive the
// width down until wrapping actually adds rows, then pin reserve == render.
func TestModalReserveTracksWrapAtNarrowWidth(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 34 // narrow enough that the hint/reason/question wrap

	base := func() int {
		m.perm, m.quest = nil, nil
		return m.baseChromeHeight()
	}()

	// A perm modal with a long policy reason wraps well past the 7 nominal rows.
	m.perm = &permReq{name: "bash", args: `{"command":"curl https://example.com/very/long/path"}`,
		reason: "this command performs network egress to an external host and was flagged by policy"}
	rendered := lipgloss.Height(m.permView())
	if rendered <= 7 {
		t.Fatalf("test precondition: expected wrapping past 7 rows at width %d, got %d", m.width, rendered)
	}
	if got := m.baseChromeHeight(); got != base+rendered {
		t.Fatalf("perm reserve must equal rendered height: base=%d rendered=%d got=%d", base, rendered, got)
	}

	// A quest modal with a long question and long options likewise wraps.
	m.perm = nil
	m.quest = &questReq{
		question: "which storage backend should the service use for durable session state under load?",
		options:  []string{"redis with append-only persistence", "in-memory with periodic snapshotting"},
	}
	rendered = lipgloss.Height(m.questView())
	if rendered <= len(m.quest.options)+4 {
		t.Fatalf("test precondition: expected question/options to wrap, got %d rows", rendered)
	}
	if got := m.baseChromeHeight(); got != base+rendered {
		t.Fatalf("quest reserve must equal rendered height: base=%d rendered=%d got=%d", base, rendered, got)
	}
}
