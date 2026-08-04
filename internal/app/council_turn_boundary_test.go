package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn begins when the USER speaks. magi injects prompt.submitted events of its own — the stall
// nudge, a plugin note, a permission message, a hook, an orchestrator re-prompt — and five scans in
// this file treated every one of them as a turn boundary, wiping the evidence they had collected.
//
// The council is the gate that decides whether work is finished, and this is the block it judges
// on. Measured before the fix: a turn whose three results included two FAILING `cleanup ran!` lines
// came back as one line once a nudge landed in the middle, and as nothing at all when the council
// was convened straight after one. The obstacles block emptied with it.
//
// lastUserPromptTS in the same file has always asked for ActorUser; the others had drifted.
func TestSystemPromptsAreNotTurnBoundaries(t *testing.T) {
	mk := func(ty event.Type, actor event.Actor, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Type: ty, Actor: actor, Data: b}
	}
	call := func(id, name string) event.Event {
		return mk(event.TypePartAppended, event.Actor{Kind: event.ActorAgent, ID: "coder"},
			event.PartAppendedData{Part: session.Part{Kind: session.PartToolCall,
				ToolCall: &session.ToolCall{CallID: id, Name: name}}})
	}
	result := func(id, text string, isErr bool) event.Event {
		c, _ := json.Marshal(text)
		return mk(event.TypePartAppended, event.Actor{Kind: event.ActorAgent, ID: "coder"},
			event.PartAppendedData{Part: session.Part{Kind: session.PartToolResult,
				ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr}}})
	}
	prompt := func(actor event.Actor, text string) event.Event {
		return mk(event.TypePromptSubmitted, actor,
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	}
	user := event.Actor{Kind: event.ActorUser, ID: "tui"}

	base := []event.Event{
		prompt(user, "make the tasks clean up on cancel"),
		call("c1", "bash"), result("c1", "exit 1\nTask 0 cleanup ran!", true),
		call("c2", "bash"), result("c2", "exit 1\nTask 1 cleanup ran!", true),
		call("c3", "write"), result("c3", "wrote 200 bytes", false),
	}
	// Every system-injected prompt magi emits, by the actor it uses.
	for _, injected := range []event.Actor{
		{Kind: event.ActorSystem, ID: "loop"},         // the stall / repeat nudge
		{Kind: event.ActorSystem, ID: "plugin"},       // a plugin note
		{Kind: event.ActorSystem, ID: "hook"},         // a hook message
		{Kind: event.ActorSystem, ID: "orchestrator"}, // a re-prompt
	} {
		for _, trailing := range []struct {
			name string
			evs  []event.Event
		}{
			{"with a call after it", []event.Event{call("c4", "read"), result("c4", "1\tfoo", false)}},
			{"with nothing after it", nil},
		} {
			evs := append(append([]event.Event{}, base...), prompt(injected, "magi says something"))
			evs = append(evs, trailing.evs...)

			got := turnToolEvidence(evs, 12)
			for _, want := range []string{"Task 0 cleanup ran!", "Task 1 cleanup ran!", "wrote 200 bytes"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s/%s: the council lost %q from its evidence:\n%s",
						injected.ID, trailing.name, want, got)
				}
			}
			if stuckEvidence(evs, 12) == "" {
				t.Errorf("%s/%s: the obstacles this turn hit went missing too", injected.ID, trailing.name)
			}
		}
	}

	// A real user prompt still starts a new turn: the earlier work is another turn's.
	next := append(append([]event.Event{}, base...),
		prompt(user, "now do something else"),
		call("c9", "read"), result("c9", "1\tbar", false))
	got := turnToolEvidence(next, 12)
	if strings.Contains(got, "Task 0 cleanup ran!") {
		t.Errorf("a USER prompt is a turn boundary and the previous turn must drop out:\n%s", got)
	}
	if !strings.Contains(got, "1\tbar") {
		t.Errorf("the new turn's own call must be there:\n%s", got)
	}
}

// The two evidence renderers share one body and differ on a single flag: whether a user prompt
// starts the window over. That difference is the entire reason there are two of them, and it is
// the one thing a wrong argument would silently invert — every other assertion in the suite passes
// either way. So it gets its own test.
//
// deltaToolEvidence's window IS the delta since the last rejection, and a user prompt inside it is
// not a boundary: resetting there would hand the re-deliberation an empty block and let work the
// council already rejected pass unexamined.
func TestOnlyTheTurnWindowResetsOnAUserPrompt(t *testing.T) {
	mk := func(ty event.Type, actor event.Actor, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Type: ty, Actor: actor, Data: b}
	}
	agent := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	call := func(id, name string) event.Event {
		return mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name}}})
	}
	result := func(id, text string) event.Event {
		c, _ := json.Marshal(text)
		return mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c}}})
	}

	evs := []event.Event{
		call("c1", "bash"), result("c1", "before the prompt"),
		mk(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
			event.PromptSubmittedData{Parts: []session.Part{{Kind: session.PartText, Text: "and now this"}}}),
		call("c2", "bash"), result("c2", "after the prompt"),
	}

	if got := turnToolEvidence(evs, 12); strings.Contains(got, "before the prompt") {
		t.Errorf("the turn window must restart at a user prompt:\n%s", got)
	}
	got := deltaToolEvidence(evs, 12)
	if !strings.Contains(got, "before the prompt") {
		t.Errorf("the delta window must NOT restart at a user prompt — the rejected work is gone:\n%s", got)
	}
	if !strings.Contains(got, "after the prompt") {
		t.Errorf("the delta window dropped the work that followed:\n%s", got)
	}
}
