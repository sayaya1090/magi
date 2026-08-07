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
		ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{
			Usage: event.Usage{In: 40000, Cached: 32000, CacheReported: true}}),
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
	// The cache reading goes with it. It described the prompt that was sent before the fold, and a
	// share of a context that no longer exists is not a share of anything.
	if got.CacheReported || got.Cached != 0 {
		t.Errorf("a pre-compaction cache reading survived the fold: %+v", got)
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

// Whether the prompt cache is working, when the backend says — and silence when it does not.
//
// Zero and silence are different facts. A backend reporting `cached: 0` is saying the cache missed;
// one that reports no cache field is saying nothing, and a screen drawing both as 0% would report a
// working cache as broken. The default local backend is the second case (measured: Ollama's /v1
// sends prompt/completion/total and no details block), so silence is the common answer.
func TestTheCacheReadingSeparatesZeroFromSilence(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name         string
		usage        event.Usage
		wantCached   int
		wantReported bool
	}{
		{"a backend that reports a hit", event.Usage{In: 5000, Cached: 4000, CacheReported: true}, 4000, true},
		{"a backend that reports a miss", event.Usage{In: 5000, Cached: 0, CacheReported: true}, 0, true},
		{"a backend that says nothing", event.Usage{In: 5000}, 0, false},
	} {
		sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range []event.Event{
			ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m1",
				Parts: []session.Part{{Kind: session.PartText, Text: "do the thing"}}}),
			ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{Usage: tc.usage}),
		} {
			if _, aerr := a.store.Append(ctx, sid, e); aerr != nil {
				t.Fatal(aerr)
			}
		}
		got, err := a.ContextStateOf(ctx, sid)
		if err != nil {
			t.Fatal(err)
		}
		if got.Cached != tc.wantCached || got.CacheReported != tc.wantReported {
			t.Errorf("%s: cached=%d reported=%v, want %d/%v",
				tc.name, got.Cached, got.CacheReported, tc.wantCached, tc.wantReported)
		}
	}
}
