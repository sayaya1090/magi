package tui

import (
	"strings"
	"testing"

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
