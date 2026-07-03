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
