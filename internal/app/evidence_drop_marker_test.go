package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The council's evidence block reads as "this turn's evidence" and is a TAIL: councilActionsCap is
// 8, so a turn with forty results hands over the last eight and the failing one from early on is
// simply absent. Nothing said so, and a reader cannot ask for what it does not know is missing.
//
// priorCouncilObjections in the same file documents the harm from a run that failed: five
// deliberations, each able to see only the round immediately before it, the defect raised in the
// second one never seen again, and a completion accepted on evidence that never exercised the case.
// clipEach, ten lines away, has always appended "…and N more".
func TestTheEvidenceBlockSaysWhatItLeftOut(t *testing.T) {
	mk := func(ty event.Type, actor event.Actor, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Type: ty, Actor: actor, Data: b}
	}
	agent := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	evs := []event.Event{mk(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"},
		event.PromptSubmittedData{})}
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("c%d", i)
		evs = append(evs, mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: "bash"}}}))
		c, _ := json.Marshal(fmt.Sprintf("RESULT-%02d", i))
		evs = append(evs, mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: i == 2}}}))
	}

	for _, c := range []struct {
		name string
		got  string
	}{
		{"turnToolEvidence", turnToolEvidence(evs, 8)},
		{"deltaToolEvidence", deltaToolEvidence(evs, 8)},
	} {
		if !strings.Contains(c.got, "32 earlier tool results this turn are not shown") {
			t.Errorf("%s: a tail must say how much it is a tail of:\n%s", c.name, c.got)
		}
		// The newest are still what is shown, and all of them.
		for _, want := range []string{"RESULT-33", "RESULT-40"} {
			if !strings.Contains(c.got, want) {
				t.Errorf("%s: the most recent results must be there, missing %s", c.name, want)
			}
		}
		if strings.Count(c.got, "\n") != 8 { // 8 results + the marker = 9 lines, 8 newlines
			t.Errorf("%s: the cap still holds — %d newlines", c.name, strings.Count(c.got, "\n"))
		}
	}

	// Under the cap, nothing is claimed to be missing.
	small := evs[:1+2*5]
	for _, c := range []struct {
		name string
		got  string
	}{
		{"turnToolEvidence", turnToolEvidence(small, 8)},
		{"deltaToolEvidence", deltaToolEvidence(small, 8)},
	} {
		if strings.Contains(c.got, "not shown") {
			t.Errorf("%s: five results fit in eight and nothing was dropped:\n%s", c.name, c.got)
		}
		if !strings.Contains(c.got, "RESULT-02") {
			t.Errorf("%s: the early failure is present when it fits:\n%s", c.name, c.got)
		}
	}

	// The obstacles block is a tail in the same way and now says so too.
	var walls []event.Event
	walls = append(walls, evs[0])
	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("w%d", i)
		walls = append(walls, mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: "bash"}}}))
		c, _ := json.Marshal(fmt.Sprintf("no such file: missing-%02d.c", i))
		walls = append(walls, mk(event.TypePartAppended, agent, event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: true}}}))
	}
	if got := stuckEvidence(walls, 6); !strings.Contains(got, "14 earlier obstacles are not shown") {
		t.Errorf("the obstacles block is a tail too and must say so:\n%s", got)
	}
	if got := stuckEvidence(walls[:1+2*3], 6); strings.Contains(got, "not shown") {
		t.Errorf("three obstacles fit in six:\n%s", got)
	}
}
