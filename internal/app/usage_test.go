package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// turn.finished carries what the turn COST: every request made under it, input and output both
// summed. Input used to be the LAST step's prompt — the current context size — which meant a
// multi-step turn reported one prompt where several were paid for, and the council's polls and the
// subagents' work appeared nowhere at all. The context meter still needs that last-prompt number
// and gets it from its own path (setPromptTokens), so this one means the bill.
func TestTurnUsageCumulative(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{ // step 1: a tool call + this step's usage
			{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: json.RawMessage(`{"path":"x"}`)}},
			{Type: port.ProviderUsage, Usage: &event.Usage{In: 100, Out: 5}},
			{Type: port.ProviderFinish},
		},
		{ // step 2: final text + this step's usage
			{Type: port.ProviderText, Text: "done"},
			{Type: port.ProviderUsage, Usage: &event.Usage{In: 150, Out: 7}},
			{Type: port.ProviderFinish},
		},
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	evs := submitAndDrain(t, a, wd)

	var u event.Usage
	found := false
	for _, e := range evs {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil {
				u = d.Usage
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no turn.finished")
	}
	if u.Out != 12 {
		t.Errorf("cumulative output = %d, want 12 (5+7)", u.Out)
	}
	if u.In != 250 {
		t.Errorf("input = %d, want 250 (100+150 — every request is paid for, not just the last)", u.In)
	}
}
