package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// B1 clean division: cloneConversation carries the parent's WORK into the child but strips the
// parent's own steering prompts, so the child cannot mistake the parent's original user request
// for its own delegated task (field report: a cloned child answering a stale parent prompt).
func TestCloneDivisionStripsParentPrompts(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	parent := session.SessionID("s_parent")
	child := session.SessionID("s_child")

	// Both sessions must open with a SessionCreated fact (jsonl store invariant); the child is
	// created before cloneConversation runs in the real spawn path.
	for _, sid := range []session.SessionID{parent, child} {
		scd, _ := json.Marshal(event.SessionCreatedData{Agent: "default"})
		if err := a.appendFact(ctx, sid, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "default"}, scd); err != nil {
			t.Fatal(err)
		}
	}

	userPD, _ := json.Marshal(event.PromptSubmittedData{MessageID: "mu",
		Parts: []session.Part{{Kind: session.PartText, Text: "ORIGINAL USER TASK: build the widget"}}})
	if err := a.appendFact(ctx, parent, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, userPD); err != nil {
		t.Fatal(err)
	}
	workPD, _ := json.Marshal(event.PartAppendedData{MessageID: "mw", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: "PARENT WORK: edited foo.go"}})
	_ = a.appendFact(ctx, parent, event.TypePartAppended, event.Actor{Kind: event.ActorAgent, ID: "default"}, workPD)
	notePD, _ := json.Marshal(event.PromptSubmittedData{MessageID: "mn",
		Parts: []session.Part{{Kind: session.PartText, Text: "SYSTEM NOTE: spec fidelity"}}})
	_ = a.appendFact(ctx, parent, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "planner"}, notePD)

	a.cloneConversation(ctx, parent, child)

	evs, err := a.store.Read(ctx, child, 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	for _, m := range reconstruct(evs) {
		buf.WriteString(partsText(m.Parts))
		buf.WriteString("\n")
	}
	got := buf.String()

	if strings.Contains(got, "ORIGINAL USER TASK") {
		t.Errorf("child must NOT see the parent's verbatim user request:\n%s", got)
	}
	if !strings.Contains(got, inheritedContextHeader) {
		t.Errorf("child must see the inherited-context header instead:\n%s", got)
	}
	if !strings.Contains(got, "PARENT WORK") {
		t.Errorf("child must inherit the parent's WORK (anti re-read):\n%s", got)
	}
	if strings.Contains(got, "SYSTEM NOTE") {
		t.Errorf("parent-turn system steering must be dropped from the clone:\n%s", got)
	}
}

// reframeInheritedPrompt is idempotent so a nested clone does not stack headers.
func TestReframeInheritedPromptIdempotent(t *testing.T) {
	pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m1",
		Parts: []session.Part{{Kind: session.PartText, Text: "raw request"}}})
	once, ok := reframeInheritedPrompt(pd)
	if !ok {
		t.Fatal("first reframe must succeed")
	}
	twice, ok := reframeInheritedPrompt(once)
	if !ok {
		t.Fatal("second reframe must succeed")
	}
	if string(once) != string(twice) {
		t.Errorf("reframe not idempotent:\n%s\nvs\n%s", once, twice)
	}
	var d event.PromptSubmittedData
	_ = json.Unmarshal(twice, &d)
	if len(d.Parts) != 1 || d.Parts[0].Text != inheritedContextHeader {
		t.Errorf("reframed payload wrong: %+v", d.Parts)
	}
}
