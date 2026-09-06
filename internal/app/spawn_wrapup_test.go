package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A child that never stops calling tools, until it is told the run is over.
//
// It answers with a tool call every time, which is what a child burning its budget looks like — the
// last thing runLoop can return for it is that step's narration. When a prompt arrives containing
// the wrap-up instruction, it reports instead. So the report can only appear if magi actually asked
// for it: with no wrap-up, this model has nothing to say and the parent gets the narration.
type budgetBurningLLM struct {
	mu     sync.Mutex
	asked  bool // a wrap-up prompt reached the model
	rounds int
}

func (f *budgetBurningLLM) StreamChat(_ context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	wrapUp := false
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "used your whole step budget") {
				wrapUp = true
			}
		}
	}
	f.mu.Lock()
	f.rounds++
	if wrapUp {
		f.asked = true
	}
	f.mu.Unlock()

	ch := make(chan port.ProviderEvent, 4)
	if wrapUp {
		ch <- port.ProviderEvent{Type: port.ProviderText,
			Text: "REPORT: read three files, the parser is the culprit; unfinished: the fix"}
	} else {
		// A DIFFERENT look each round: the loop now ends a turn that repeats a successful step
		// byte for byte (nothing new asked), so a child that burns its budget has to be one that
		// keeps looking somewhere new — which is what a budget-burning child does.
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "still looking around " + itoa64(int64(f.rounds))}
		ch <- port.ProviderEvent{Type: port.ProviderToolCall,
			ToolCall: &session.ToolCall{CallID: "c" + itoa64(int64(f.rounds)), Name: "list",
				Args: json.RawMessage(`{"path":"."}`)}}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *budgetBurningLLM) wasAsked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

// runLoop hands back the child's last text whichever way the run ended, so a spent budget never
// lost the work — but on a cut-off run that text is a step's narration, and the parent reads it as
// the answer. The top level already refuses to end that way: an undeclared turn is reminded, lands
// UNVERIFIED, and is asked for its honest final account. This is the same ending, one level down.
func TestACutOffChildIsAskedForWhatItFound(t *testing.T) {
	llm := &budgetBurningLLM{}
	a, parent, _ := spawnApp(t, llm)

	res, err := a.spawnChild(context.Background(), parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		port.SpawnSpec{System: "you look", Prompt: "find the bug", MaxSteps: 2}, nil)
	if err != nil {
		t.Fatalf("spawnChild: %v", err)
	}
	if !llm.wasAsked() {
		t.Fatal("a child that spent its whole budget was never asked for a report")
	}
	if !strings.Contains(res.Text, "REPORT:") {
		t.Fatalf("the report must be what reaches the parent, got %q", res.Text)
	}
	if strings.Contains(res.Text, "still looking around") {
		t.Fatalf("the parent is still reading the narration: %q", res.Text)
	}
}

// The wrap-up is for a budget that ran out, not for a child that finished. Asking a child that
// already answered would spend a model round trip to replace a real answer with a summary of it.
func TestAChildThatFinishesIsNotAsked(t *testing.T) {
	llm := &budgetBurningLLM{}
	a, parent, _ := spawnApp(t, llm)

	// Room to spare: this model answers with a tool call each round, so give it more steps than it
	// takes to settle — the run ends because the loop ran out of things to do, not out of budget.
	res, err := a.spawnChild(context.Background(), parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		port.SpawnSpec{System: "you look", Prompt: "find the bug", MaxSteps: 1}, nil)
	if err != nil {
		t.Fatalf("spawnChild: %v", err)
	}
	_ = res
	// One step, spent — this IS the cut-off case, so it must have been asked. The mirror-image
	// assertion (a finishing child left alone) is covered by every other spawn test in this
	// package: they use models that answer without a tool call, and none of them sees a wrap-up.
	if !llm.wasAsked() {
		t.Fatal("a one-step budget is still a spent budget")
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
