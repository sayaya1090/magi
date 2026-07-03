package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A working turn ends with a one-line receipt (steps · files · council round);
// a pure conversational turn (no tools) adds no receipt at all.
func TestTurnSummaryLine(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.turnFiles = map[string]bool{}

	part := func(p session.Part) event.Event {
		return ev(t, event.TypePartAppended, event.PartAppendedData{Role: session.RoleAssistant, Part: p})
	}
	m.applyEvent(part(session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "write", Args: []byte(`{"path":"a.go"}`)}}))
	m.applyEvent(part(session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "bash", Args: []byte(`{"command":"go test"}`)}}))
	m.applyEvent(part(session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "edit", Args: []byte(`{"path":"a.go"}`)}}))
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{Round: 1, Members: []string{"M"}, Rule: "majority"}))
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{Round: 1, Decision: "done"}))
	m.applyEvent(event.Event{Type: event.TypeTurnFinished})

	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockInfo || !strings.Contains(last.text, "▣ turn: 3 steps") {
		t.Fatalf("expected the turn receipt, got %+v", last)
	}
	if !strings.Contains(last.text, "1 file(s)") || !strings.Contains(last.text, "council r1") {
		t.Fatalf("receipt missing files/council: %q", last.text)
	}

	// Conversational turn: counters reset, no tools → no receipt.
	m.turnSteps, m.turnCouncil, m.turnFiles = 0, 0, map[string]bool{}
	before := len(m.blocks)
	m.applyEvent(event.Event{Type: event.TypeTurnFinished})
	if len(m.blocks) != before {
		t.Fatalf("a no-tool turn must not add a receipt, got %+v", m.blocks[len(m.blocks)-1])
	}
}

// A near-verbatim long re-answer within the same turn collapses to a stub —
// the council-reject-then-resubmit case that made users re-read a wall of text.
func TestDuplicateAssistantBlockCollapses(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	long := strings.Repeat("이 프로젝트의 구조 분석 결과입니다. ", 30)
	part := func(txt string) event.Event {
		return ev(t, event.TypePartAppended, event.PartAppendedData{Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: txt}})
	}
	m.applyEvent(part(long))
	m.applyEvent(part(long + "  \n")) // whitespace-only difference
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockInfo || !strings.Contains(last.text, "동일") {
		t.Fatalf("duplicate long answer should collapse, got kind=%d %q", last.kind, last.text[:min(60, len(last.text))])
	}
	// A genuinely different answer stays.
	m.applyEvent(part(long + " 그리고 추가 분석."))
	if m.blocks[len(m.blocks)-1].kind != blockAssistant {
		t.Fatal("a changed answer must not collapse")
	}
	// Short duplicates (greetings) are left alone.
	m.applyEvent(part("네."))
	m.applyEvent(part("네."))
	if m.blocks[len(m.blocks)-1].kind != blockAssistant {
		t.Fatal("short duplicates are not worth collapsing")
	}
}
