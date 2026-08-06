package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn ends once. The teardown used to emit a terminal event whenever the run context was dead,
// which a headless one-shot makes true right after a normal finish — so every recorded session
// carried two turn.finished, the second with {"in":0,"out":0}. Anything reading the LAST one (the
// fork boundary, the token meter) then read a turn that spent nothing.
//
// Measured on fix-git__b8qg3R8 (reward 1): 06:51:28 turn.finished in=104292, and 06:51:28
// turn.finished in=0. Two of two runs, every session.
func TestTurnFinishesExactlyOnce(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, &usageLLM{text: "hi", in: 1234, out: 7}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "say hi"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "cli"},
	}); err != nil {
		t.Fatal(err)
	}
	// Close the INSTANT the terminal event arrives, which is what a headless one-shot does: it
	// breaks out of the event stream on turn.finished and its deferred Close cancels the run
	// context while the goroutine is still tearing the turn down.
	sub, unsub, err := a.Subscribe(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	deadline := time.After(20 * time.Second)
	for done := false; !done; {
		select {
		case e := <-sub:
			if e.Type == event.TypeTurnFinished {
				done = true
			}
		case <-deadline:
			t.Fatal("the turn never finished")
		}
	}
	cc, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Close(cc); err != nil {
		t.Fatalf("close: %v", err)
	}

	evs, err := store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var finished []event.Usage
	for _, e := range evs {
		if e.Type != event.TypeTurnFinished {
			continue
		}
		var d event.TurnFinishedData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		finished = append(finished, d.Usage)
	}
	if len(finished) != 1 {
		t.Fatalf("a turn must end once, got %d turn.finished events: %+v", len(finished), finished)
	}
	if finished[0].In == 0 {
		t.Errorf("the one terminal event must carry the turn's real usage, got %+v", finished[0])
	}
}
