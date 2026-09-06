package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A step that repeats the previous one exactly, after that step's calls all succeeded, asks for
// nothing new: the turn ends there, the calls are not run again, and the record says why. Live
// (Excel, 2026-09-07): seven identical "text + land" steps after land had already answered.
func TestAnExactRepeatOfASuccessfulStepEndsTheTurn(t *testing.T) {
	said := func() []port.ProviderEvent { // "text + call", the shape the live turn repeated
		return append([]port.ProviderEvent{{Type: port.ProviderText, Text: "시트 목록: Sheet1 (1개)"}},
			toolStep("read", `{"path":"a.txt"}`)...)
	}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		said(),
		said(), // byte-identical to the step before
		said(), // never reached
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "read it"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("the turn did not finish once: %v", typesOf(got))
	}
	results, noted := 0, false
	for _, e := range got {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), `"toolResult"`) {
			results++
		}
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "repeated the previous one exactly") {
			noted = true
		}
	}
	if results != 1 {
		for _, e := range got {
			if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), `"toolResult"`) {
				t.Logf("result: %s", clipLine(string(e.Data), 300))
			}
			if e.Type == event.TypePromptSubmitted {
				t.Logf("prompt seq=%d: %s", e.Seq, clipLine(string(e.Data), 120))
			}
		}
		t.Errorf("the repeated call was run again: %d results", results)
	}
	if !noted {
		t.Error("the record does not say why the turn ended")
	}
}

// A repeat after a FAILURE is a retry, and runs.
func TestARepeatAfterAFailureStillRuns(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"missing.txt"}`),
		toolStep("read", `{"path":"missing.txt"}`),
		textStep("gave up"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "read it"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	results := 0
	for _, e := range got {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), `"toolResult"`) {
			results++
		}
	}
	if results != 2 {
		t.Errorf("a retry after a failure was suppressed: %d results", results)
	}
}

// The predicate's two escapes: a prompt that arrived in between is new information, and a tool
// whose repeat has its own meaning (a declaration for the council, a wait, a poll, a question).
func TestARepeatIsNotChurnWhenSomethingArrivedOrTheCallIsMeantToRepeat(t *testing.T) {
	call := func(name string) []*session.ToolCall {
		return []*session.ToolCall{{CallID: "c1", Name: name, Args: json.RawMessage(`{"a":1}`)}}
	}
	base := &turnState{lastStep: stepSignature("same", call("read")), lastCalls: []string{"c1"}, lastPromptSeq: 5}
	if !repeatIsChurn(base, stepSignature("same", call("read")), call("read"), 5, nil) {
		t.Fatal("an exact repeat with nothing new and no failure is churn")
	}
	if repeatIsChurn(base, stepSignature("same", call("read")), call("read"), 9, nil) {
		t.Error("a prompt arrived between the steps — the repeat may be a response to it")
	}
	for _, name := range []string{"council", "wait_for", "bash_output", "ask_user", "hand_off"} {
		ts := &turnState{lastStep: stepSignature("same", call(name)), lastCalls: []string{"c1"}, lastPromptSeq: 5}
		if repeatIsChurn(ts, stepSignature("same", call(name)), call(name), 5, nil) {
			t.Errorf("%s repeated identically was called churn — its repeat has its own meaning", name)
		}
	}
	if repeatIsChurn(base, stepSignature("different words", call("read")), call("read"), 5, nil) {
		t.Error("different text is not a repeat")
	}
}
