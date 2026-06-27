package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func mkEvent(ty event.Type, actor event.ActorKind, stage string, data any) event.Event {
	b, _ := json.Marshal(data)
	return event.Event{Type: ty, Actor: event.Actor{Kind: actor}, Stage: stage, Data: b}
}

func TestBuildLoopMap(t *testing.T) {
	evs := []event.Event{
		mkEvent(event.TypePromptSubmitted, event.ActorUser, stageExecute,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "build the parser"}}}),
		// step 1 (m1): text + a tool call
		mkEvent(event.TypePartAppended, event.ActorAgent, stageExecute,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "ok"}}),
		mkEvent(event.TypePartAppended, event.ActorAgent, stageExecute,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read"}}}),
		// a failing tool result
		mkEvent(event.TypePartAppended, event.ActorAgent, stageExecute,
			event.PartAppendedData{MessageID: "mt", Role: session.RoleTool, Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{IsError: true}}}),
		// step 2 (m2): text only
		mkEvent(event.TypePartAppended, event.ActorAgent, stageExecute,
			event.PartAppendedData{MessageID: "m2", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "done"}}),
		// council: continue then done
		mkEvent(event.TypeCouncilDecided, event.ActorSystem, stageCouncil,
			event.CouncilDecidedData{Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Done: 1, Continue: 2}}),
		mkEvent(event.TypeCouncilDecided, event.ActorSystem, stageCouncil,
			event.CouncilDecidedData{Round: 2, Decision: string(council.Done), Tally: council.Breakdown{Done: 3}}),
		mkEvent(event.TypeTurnFinished, event.ActorAgent, stageFinalize,
			event.TurnFinishedData{Usage: event.Usage{In: 1000, Out: 200}}),
	}

	m := buildLoopMap(evs)
	wants := []string{
		"1 turn(s)",
		"Turn 1: build the parser",
		"2 steps",
		"1 tool call",
		"1 error",
		"r1 1✓/2→ continue",
		"r2 3✓/0→ done",
		"1000 in / 200 out",
	}
	for _, w := range wants {
		if !strings.Contains(m, w) {
			t.Errorf("loop map missing %q\n---\n%s", w, m)
		}
	}
}

func TestBuildLoopMapEmpty(t *testing.T) {
	if got := buildLoopMap(nil); !strings.Contains(got, "no turns") {
		t.Errorf("empty map = %q", got)
	}
}

// A system-injected prompt (council feedback) does NOT start a new turn.
func TestBuildLoopMapSystemPromptStaysInTurn(t *testing.T) {
	evs := []event.Event{
		mkEvent(event.TypePromptSubmitted, event.ActorUser, stageExecute,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "task"}}}),
		mkEvent(event.TypePromptSubmitted, event.ActorSystem, stageCouncil,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "council feedback"}}}),
	}
	if m := buildLoopMap(evs); !strings.Contains(m, "1 turn(s)") {
		t.Errorf("system prompt should not start a new turn:\n%s", m)
	}
}
