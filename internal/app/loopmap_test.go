package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func mkEvent(ty event.Type, actor event.ActorKind, data any) event.Event {
	b, _ := json.Marshal(data)
	return event.Event{Type: ty, Actor: event.Actor{Kind: actor}, Data: b}
}

func TestBuildLoopMap(t *testing.T) {
	evs := []event.Event{
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "build the parser"}}}),
		// step 1 (m1): text + a tool call
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "ok"}}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read"}}}),
		// a failing tool result
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "mt", Role: session.RoleTool, Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{IsError: true}}}),
		// step 2 (m2): text only
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m2", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "done"}}),
		// council: continue then done
		mkEvent(event.TypeCouncilDecided, event.ActorSystem,
			event.CouncilDecidedData{Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Done: 1, Continue: 2}}),
		mkEvent(event.TypeCouncilDecided, event.ActorSystem,
			event.CouncilDecidedData{Round: 2, Decision: string(council.Done), Tally: council.Breakdown{Done: 3}}),
		mkEvent(event.TypeTurnFinished, event.ActorAgent,
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
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "task"}}}),
		mkEvent(event.TypePromptSubmitted, event.ActorSystem,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "council feedback"}}}),
	}
	if m := buildLoopMap(evs); !strings.Contains(m, "1 turn(s)") {
		t.Errorf("system prompt should not start a new turn:\n%s", m)
	}
}

// A single assistant message with MULTIPLE tool-call parts is ONE step (steps dedup by MessageID) but
// its tool COUNT reflects every call — so a multi-tool step reads "1 step · 2 tool calls", not two steps.
func TestBuildLoopMapMultiToolPerMessage(t *testing.T) {
	evs := []event.Event{
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read"}}}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "grep"}}}),
	}
	m := buildLoopMap(evs)
	if !strings.Contains(m, "1 step · 2 tool calls") {
		t.Errorf("two tool calls in one message must read '1 step · 2 tool calls':\n%s", m)
	}
}

// The loop map's counts are post-rebuttal like every other surface's, so a round argued from
// 1-2 into 3-0 renders identically to one nobody disagreed about. The marker separates them:
// ⇄N for members who moved, ! when the outcome itself turned over.
func TestLoopMapMarksAnArguedRound(t *testing.T) {
	render := func(d *council.DebateOutcome) string {
		return buildLoopMap([]event.Event{
			mkEvent(event.TypePromptSubmitted, event.ActorUser,
				event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "go"}}}),
			mkEvent(event.TypeCouncilDecided, event.ActorSystem,
				event.CouncilDecidedData{Round: 1, Decision: string(council.Done),
					Tally: council.Breakdown{Done: 3, Voters: 3}, Debate: d}),
		})
	}
	if got := render(nil); strings.Contains(got, "⇄") {
		t.Errorf("no rebuttal ran, so there is nothing to mark: %q", got)
	}
	got := render(&council.DebateOutcome{Before: council.Done, After: council.Done, Changed: 1})
	if !strings.Contains(got, "⇄1") || strings.Contains(got, "⇄1!") {
		t.Errorf("members moved but the outcome held — want ⇄1 with no flip mark: %q", got)
	}
	if got := render(&council.DebateOutcome{Before: council.Continue, After: council.Done, Changed: 2}); !strings.Contains(got, "⇄2!") {
		t.Errorf("the rebuttal turned the outcome over — want ⇄2!: %q", got)
	}
}
