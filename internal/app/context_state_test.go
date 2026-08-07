package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func ctxEvent(t *testing.T, typ event.Type, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b}
}

// A compaction is the one moment a companion silently stops knowing something. Reading it back out
// of the log is what makes that visible to somebody supervising, and the log already has it — so
// this answers for sessions that ran long before anybody asked.
func TestContextStateReadsTheFoldsOutOfTheLog(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []event.Event{
		ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "do the thing"}}}),
		ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{Usage: event.Usage{In: 40000}}),
		ctxEvent(t, event.TypeCompaction, event.CompactionData{
			Summary: "we looked at the parser", TokensBefore: 40000, TokensAfter: 9000,
			Shards: []event.ContextShard{{Topic: "internal/parse.go"}, {Topic: "discussion"}}}),
	} {
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := a.ContextStateOf(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compactions != 1 || got.Shed != 31000 {
		t.Errorf("the fold came back as %+v", got)
	}
	if len(got.Topics) != 2 || got.Topics[0] != "internal/parse.go" {
		t.Errorf("the recallable topics came back as %v", got.Topics)
	}
	// The 40,000 was measured before the fold and describes a context that no longer exists — and
	// it is the LARGER number, so carrying it across would report a companion as nearly full at
	// the moment it was emptied.
	if !got.Estimated {
		t.Errorf("a pre-compaction measurement was reported as the current size: %+v", got)
	}
	if got.Used >= 40000 {
		t.Errorf("used is %d after a fold from 40000 to 9000", got.Used)
	}

	// A turn that finishes after the fold measures the context that exists now, and displaces
	// the estimate.
	if _, err := a.store.Append(ctx, sid, ctxEvent(t, event.TypeTurnFinished,
		event.TurnFinishedData{Usage: event.Usage{In: 9500}})); err != nil {
		t.Fatal(err)
	}
	got, err = a.ContextStateOf(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Estimated || got.Used != 9500 {
		t.Errorf("the post-fold measurement was not used: %+v", got)
	}
}

// Several backends report no usage at all. A zero is not a measurement, and taking one would say a
// full session was empty.
func TestAnUnreportedUsageDoesNotBecomeZeroTokens(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []event.Event{
		ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "a fairly long instruction that certainly costs more than nothing to send"}}}),
		ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{}),
	} {
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := a.ContextStateOf(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Estimated || got.Used == 0 {
		t.Errorf("a silent backend emptied the context: %+v", got)
	}

	// And a silent turn after a reported one does not undo the measurement either: the context did
	// not shrink because the backend stopped saying how big it was.
	for _, e := range []event.Event{
		ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{Usage: event.Usage{In: 7000}}),
		ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{}),
	} {
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}
	if got, err = a.ContextStateOf(ctx, sid); err != nil {
		t.Fatal(err)
	} else if got.Estimated || got.Used != 7000 {
		t.Errorf("a silent turn threw away the last measurement: %+v", got)
	}
}
