package jsonl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A full sync costs ~4ms on macOS (F_FULLFSYNC) and is heavy on Windows (FlushFileBuffers); a step
// wrote ~5 events and synced each (measured 2026-11-16: step gap 15ms vs 1.7ms without). Every event
// still reaches the OS before Append returns, so a killed or crashed process loses nothing — the
// kernel holds it. Only a power cut can drop what was not synced, so the store syncs where a turn
// begins or ends, on an error, and where a person settles something.
func TestAppendSyncsOnlyAtTurnBoundaries(t *testing.T) {
	var n int
	prev := syncFile
	syncFile = func(f *os.File) error { n++; return prev(f) }
	t.Cleanup(func() { syncFile = prev })

	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	sid := session.SessionID("s_sync")
	step := func(want int, evs ...event.Event) {
		t.Helper()
		n = 0
		if _, err := s.Append(ctx, sid, evs...); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s: %d syncs, want %d", evs[0].Type, n, want)
		}
	}
	step(1, created(wd, now))
	step(1, fact(event.TypePromptSubmitted, now))
	for _, typ := range []event.Type{event.TypePartAppended, event.TypePermissionDecided, event.TypeToolStarted, event.TypePartAppended} {
		step(0, fact(typ, now))
	}
	step(1, fact(event.TypeError, now))
	step(1, fact(event.TypeToolResolved, now))
	step(1, fact(event.TypeTurnFinished, now))
	step(1, fact(event.TypePartAppended, now), fact(event.TypeTurnFinished, now))

	// What was not synced is still written: a second reader of the same files sees all of it.
	other, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := other.Read(ctx, sid, 0)
	if err != nil || len(evs) != 11 {
		t.Fatalf("a fresh reader saw %d events (err %v), want 11", len(evs), err)
	}
}
