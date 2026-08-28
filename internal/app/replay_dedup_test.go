package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

// F-STORE-READ-REPLAY: a fact that was replayed out of the log is not delivered a second time when
// the bus hands it over too.
//
// Subscribe takes the bus BEFORE it reads the log, on purpose: an event appended in between is then
// caught by both halves rather than by neither. That ordering is what makes the overlap normal
// instead of exceptional, and the de-duplication is the only thing standing between it and a
// screen that draws the same line twice. Every existing subscriber test resumes at seq 0 against a
// session that is not being written to, where the overlap is empty and the rule is never exercised;
// production only ever attaches mid-log, to a session that IS being written to.
//
// The boundary is the whole rule. `<= maxSeq` and `< maxSeq` differ on exactly the last replayed
// event — which is the one most likely to be in flight — so the off-by-one leaks precisely the
// event the reader was resuming at.
func TestAReplayedFactIsNotDeliveredTwiceWhenTheBusRepeatsIt(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var last event.Event
	for i := 0; i < 4; i++ {
		e := ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{})
		seqs, err := a.store.Append(ctx, sid, e)
		if err != nil {
			t.Fatal(err)
		}
		e.Seq, e.SessionID = seqs[len(seqs)-1], sid
		last = e
	}

	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ch, unsub, err := a.Subscribe(sctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	// What the window between bus.Subscribe and store.Read produces: the log's newest event, already
	// replayed, arriving on the bus as well. Publishing it directly is that situation without the race.
	go func() {
		time.Sleep(150 * time.Millisecond)
		a.bus.Publish(last)
	}()

	seen := map[int64]int{}
	deadline := time.After(1500 * time.Millisecond)
drain:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				break drain
			}
			if e.Type == event.TypeTurnFinished {
				seen[e.Seq]++
			}
		case <-deadline:
			break drain
		}
	}
	if n := seen[last.Seq]; n != 1 {
		t.Errorf("seq %d was delivered %d times, want 1 — it was replayed from the log and the bus "+
			"handed it over again; a reader resuming at that cursor draws the line twice", last.Seq, n)
	}
}

// The other half of the same gap: resuming from a cursor in the MIDDLE of the log.
//
// Measured 2026-08-29: of the ~20 App.Subscribe calls in tests, every one but a rewind passes
// fromSeq 0. Both production callers — `magi attach` and the hand server — pass a cursor a reader
// already holds, which is never 0 after the first frame. The path the product always takes was the
// path nothing ran.
//
// Two ways to be wrong at a cursor, and this pins both: send the cursor's own event again (the
// reader sees a line twice) or skip the one after it (the reader silently loses a line and has no
// way to know).
func TestResumingFromACursorInTheMiddleSendsExactlyWhatComesAfterIt(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for i := 0; i < 6; i++ {
		s, err := a.store.Append(ctx, sid, ctxEvent(t, event.TypeTurnFinished, event.TurnFinishedData{}))
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, s[len(s)-1])
	}
	cursor := seqs[2]

	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ch, unsub, err := a.Subscribe(sctx, sid, cursor)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	// And the stream stays live across the seam: one more fact after the replay is drained.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = a.appendFact(ctx, sid, event.TypeTurnFinished, event.Actor{}, []byte(`{}`))
	}()

	var got []int64
	deadline := time.After(1500 * time.Millisecond)
drain:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				break drain
			}
			if e.Type == event.TypeTurnFinished {
				got = append(got, e.Seq)
			}
		case <-deadline:
			break drain
		}
	}
	want := []int64{seqs[3], seqs[4], seqs[5], seqs[5] + 1}
	if len(got) != len(want) {
		t.Fatalf("resumed at %d and got seqs %v, want %v", cursor, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resumed at %d and got seqs %v, want %v", cursor, got, want)
		}
	}
}
