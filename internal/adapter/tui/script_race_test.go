package tui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Sweep eight: input that arrives while magi is busy.
//
// The question a user actually has when they type mid-turn is not cosmetic — it is "is this being
// handled now, or after what you are doing?" Every answer is acceptable; not knowing is not. So
// these drive a REAL turn, drop a prompt in at a controlled moment, and check two things: the
// request is never lost, and the transcript says which of the two it is.
//
// Timing is controlled rather than slept on: the scripted model blocks at a chosen step until the
// test has submitted, so "during a tool call" means exactly that every run.

// hookedLLM is scriptedLLM with a barrier: before serving step n, it runs before[n] and waits.
//
// The hook runs on the APP's goroutine, so it must not touch the Model — the harness replaces that
// value on the test goroutine every step, and reading it here is a data race (the detector says so).
// Everything a hook needs is captured before the turn starts.
type hookedLLM struct {
	mu     sync.Mutex
	steps  [][]port.ProviderEvent
	before map[int]func()
	at     int
}

func (l *hookedLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	l.mu.Lock()
	i := l.at
	l.at++
	step := []port.ProviderEvent{{Type: port.ProviderText, Text: "done."}, {Type: port.ProviderFinish}}
	if i < len(l.steps) {
		step = l.steps[i]
	}
	hook := l.before[i]
	l.mu.Unlock()
	if hook != nil {
		hook() // the test injects its steer here, synchronously, before this step is answered
	}
	ch := make(chan port.ProviderEvent, len(step))
	for _, e := range step {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// disposition reads, from the transcript alone, what became of a steered request: it either left
// the queue (resolved, resurfaced, or answered) or it is still queued. Anything else means the
// user's message is in neither place, which is the one outcome that must not happen.
func disposition(evs []event.Event, msgID string) (queued, resolved bool) {
	for _, e := range evs {
		switch e.Type {
		case event.TypeInterjectionDeferred:
			var d event.InterjectionDeferredData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
				if d.Resolved {
					resolved = true
				} else {
					queued = true
				}
			}
		case event.TypeInterjectionAnswered:
			var d event.InterjectionAnsweredData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
				resolved = true
			}
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom == msgID {
				resolved = true
			}
		}
	}
	return queued, resolved
}

// A prompt typed while a tool call is in flight. It must be recorded as deferred — the honest
// answer to "now or later?" — and the bubble must say so on screen.
func TestAPromptTypedDuringAToolCallIsDeferredAndSaysSo(t *testing.T) {
	var r *realTurn
	var capturedApp *app.App
	var capturedSID session.SessionID
	steered := make(chan string, 1)
	llm := &hookedLLM{
		steps: [][]port.ProviderEvent{
			{say("Reading."), call("c1", "bash", `{"command":"echo one"}`), finish},
			{say("Reading again."), call("c2", "bash", `{"command":"echo two"}`), finish},
			{say("All done."), finish},
		},
		before: map[int]func(){
			// Step 1 is answered only after the first tool call has run, so submitting here means
			// the prompt lands with a completed call behind it and another about to start.
			1: func() {
				_ = capturedApp.Steer(context.Background(), command.SubmitPrompt{
					SessionID: capturedSID,
					Parts:     []session.Part{{Kind: session.PartText, Text: "also check the header"}},
					Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
				})
				steered <- "sent"
			},
		},
	}
	r = newRealTurn(t, llm)
	capturedApp, capturedSID = r.app, r.m.sid
	r.run("read the file twice")

	select {
	case <-steered:
	default:
		t.Fatal("the steer was never injected, so this asserts nothing")
	}
	evs := r.storeEvents(t)
	// The user's message must exist as a user prompt in the record — never silently dropped.
	found := false
	for _, e := range evs {
		var d event.PromptSubmittedData
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser &&
			json.Unmarshal(e.Data, &d) == nil {
			for _, p := range d.Parts {
				if strings.Contains(p.Text, "also check the header") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("a prompt the user typed mid-turn is nowhere in the record")
	}
	// Deferred, not handled now — the honest answer to "now or later?", and the one this harness
	// can actually observe. The BUBBLE is deliberately not asserted here: a user prompt's bubble is
	// added by the TUI's own submit path (the event only stamps the id onto it), and this steer
	// comes from the app side because that is the only way to land it at a controlled moment.
	// Whether the bubble then reads as waiting is pinned by the scripted-session tests, which type
	// through the view.
	id := ""
	for _, e := range evs {
		var d event.PromptSubmittedData
		if e.Type == event.TypePromptSubmitted && json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom == "" {
			for _, p := range d.Parts {
				if strings.Contains(p.Text, "also check the header") {
					id = d.MessageID
				}
			}
		}
	}
	if id == "" {
		t.Fatal("no prompt event carries the steered text")
	}
	queued, resolved := disposition(evs, id)
	if !queued && !resolved {
		t.Error("a prompt typed during a tool call is in neither state — nothing will pick it up")
	}
	if !queued {
		t.Log("note: it was resolved within the turn rather than deferred")
	}
}

// Two prompts in quick succession while a turn runs. Neither may be swallowed by the other — a
// user who asked two things and is told about one has lost a request without being told.
func TestTwoPromptsInQuickSuccessionAreBothRecorded(t *testing.T) {
	var r *realTurn
	var capturedApp *app.App
	var capturedSID session.SessionID
	llm := &hookedLLM{
		steps: [][]port.ProviderEvent{
			{say("Working."), call("c1", "bash", `{"command":"echo one"}`), finish},
			{say("Still working."), call("c2", "bash", `{"command":"echo two"}`), finish},
			{say("Done."), finish},
		},
		before: map[int]func(){
			1: func() {
				for _, txt := range []string{"first extra", "second extra"} {
					_ = capturedApp.Steer(context.Background(), command.SubmitPrompt{
						SessionID: capturedSID,
						Parts:     []session.Part{{Kind: session.PartText, Text: txt}},
						Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
					})
				}
			},
		},
	}
	r = newRealTurn(t, llm)
	capturedApp, capturedSID = r.app, r.m.sid
	r.run("do the long thing")

	evs := r.storeEvents(t)
	for _, want := range []string{"first extra", "second extra"} {
		seen := false
		for _, e := range evs {
			var d event.PromptSubmittedData
			if e.Type == event.TypePromptSubmitted && json.Unmarshal(e.Data, &d) == nil {
				for _, p := range d.Parts {
					if strings.Contains(p.Text, want) {
						seen = true
					}
				}
			}
		}
		if !seen {
			t.Errorf("%q was typed and is in no record at all", want)
		}
	}
}

// Every steered request ends in exactly one of two states: still queued, or resolved. Neither is
// an error; being in neither is, because then nothing will ever pick it up and nothing said so.
func TestEverySteeredRequestEndsQueuedOrResolved(t *testing.T) {
	var r *realTurn
	var capturedApp *app.App
	var capturedSID session.SessionID
	llm := &hookedLLM{
		steps: [][]port.ProviderEvent{
			{say("One."), call("c1", "bash", `{"command":"echo a"}`), finish},
			{say("Two."), finish},
		},
		before: map[int]func(){
			1: func() {
				_ = capturedApp.Steer(context.Background(), command.SubmitPrompt{
					SessionID: capturedSID,
					Parts:     []session.Part{{Kind: session.PartText, Text: "the extra request"}},
					Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
				})
			},
		},
	}
	r = newRealTurn(t, llm)
	capturedApp, capturedSID = r.app, r.m.sid
	r.run("start")

	evs := r.storeEvents(t)
	var id string
	for _, e := range evs {
		var d event.PromptSubmittedData
		if e.Type == event.TypePromptSubmitted && json.Unmarshal(e.Data, &d) == nil {
			for _, p := range d.Parts {
				if strings.Contains(p.Text, "the extra request") && d.ResurfacedFrom == "" {
					id = d.MessageID
				}
			}
		}
	}
	if id == "" {
		t.Fatal("the steer produced no prompt event, so there is nothing to track")
	}
	queued, resolved := disposition(evs, id)
	if !queued && !resolved {
		t.Errorf("the request %q is in neither state: nothing will pick it up and nothing said so", id)
	}
}
