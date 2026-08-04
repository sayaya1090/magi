package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// When the run goroutine retires, something must say so on the bus.
//
// Reported live. A turn finished and was recorded at 21:07:29; the drain then answered a queued
// interjection INLINE and persisted the reply at 21:07:41; eighteen minutes later the transcript
// was still showing "working…". Nothing was wrong with the transcript — real tokens arrived after
// turn.finished, so it revived the spinner, which is the right call. What was missing is anything
// that turns it back off, because the drain's inline answer emits no terminal event of its own and
// the run's own one had already been written before it.
//
// The signal is transient on purpose: a second turn.finished in the LOG is a separate known defect
// (the fork boundary and the usage meter read the last one, and a duplicate reads as a turn that
// spent nothing), so this reaches the bus and no reader of the store.
func TestRetiringTheRunSaysSoOnTheBus(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	sub, cancel, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	transient := make(chan struct{}, 4)
	go func() {
		for e := range sub {
			if e.Type == event.TypeTurnFinished && e.Seq == 0 { // seq 0 = bus-only
				select {
				case transient <- struct{}{}:
				default:
				}
			}
		}
	}()

	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hello"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "cli"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transient:
	case <-time.After(10 * time.Second):
		t.Fatal("the run retired with nothing on the bus to stop the spinner")
	}

	// And the LOG is unchanged: exactly one turn.finished, the one the turn itself wrote.
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Type == event.TypeTurnFinished {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the record carries %d turn.finished events; a duplicate is what the fork boundary and the meter misread", n)
	}
	_ = json.Marshal
}
