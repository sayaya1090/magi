package openai

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// H1: a harmony-family model plans in an analysis channel it expects to SEE again while the turn
// is still open. Behind the flag, an assistant message's reasoning travels back — but only inside
// the open turn (after the last user message); history's reasoning is dropped by the model's own
// template anyway, so sending it would buy nothing.
func TestReasoningIsResentOnlyInsideTheOpenTurn(t *testing.T) {
	t.Setenv("MAGI_RESEND_REASONING", "1")
	msgs := []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "old ask"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "OLD-ANALYSIS"},
			{Kind: session.PartText, Text: "old answer"}}},
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "new ask"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "LIVE-ANALYSIS"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: []byte(`{}`)}}}},
	}
	out := convertMessages(msgs, false)
	var live, old string
	for _, m := range out {
		switch {
		case m.Reasoning == "LIVE-ANALYSIS":
			live = m.Reasoning
		case m.Reasoning == "OLD-ANALYSIS":
			old = m.Reasoning
		}
	}
	if live == "" {
		t.Error("the open turn's reasoning must be resent")
	}
	if old != "" {
		t.Error("history's reasoning must not be resent")
	}
}

// Default ON since the paired pilot; MAGI_RESEND_REASONING=0 is the off switch, and it must
// actually switch off.
func TestReasoningResendCanBeSwitchedOff(t *testing.T) {
	t.Setenv("MAGI_RESEND_REASONING", "0")
	msgs := []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "ask"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "ANALYSIS"},
			{Kind: session.PartText, Text: "answer"}}},
	}
	for _, m := range convertMessages(msgs, false) {
		if m.Reasoning != "" {
			t.Fatalf("reasoning sent with the flag off: %q", m.Reasoning)
		}
	}
}

// And unset means ON: the open turn's reasoning travels without anyone flipping anything.
func TestReasoningResendIsOnByDefault(t *testing.T) {
	t.Setenv("MAGI_RESEND_REASONING", "")
	msgs := []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "ask"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "ANALYSIS"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: []byte(`{}`)}}}},
	}
	found := false
	for _, m := range convertMessages(msgs, false) {
		if m.Reasoning == "ANALYSIS" {
			found = true
		}
	}
	if !found {
		t.Fatal("default must resend the open turn's reasoning")
	}
}
