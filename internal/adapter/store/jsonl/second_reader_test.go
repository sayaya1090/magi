package jsonl

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A second process reads the same log while the first one writes it.
//
// This was not a supported shape until magi could run as a daemon with a viewer attached, and the
// cache was built for the world where it was: parse the file once, serve it from memory. So the
// viewer showed the transcript as it stood the moment it first looked, forever — a live page that
// was live only in the sense that it kept redrawing the same thing. Observed in the browser before
// it was found here.
//
// Two Store values over one directory is exactly that arrangement, and it is what these tests use.

func mkEvent(t *testing.T, typ event.Type, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b}
}

// twoStores returns a writer and a reader over one root, and the session they share.
func twoStores(t *testing.T) (writer, reader *Store, sid session.SessionID) {
	t.Helper()
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	sid = session.SessionID("s_shared")
	if _, err := w.Append(context.Background(), sid,
		mkEvent(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w"}),
	); err != nil {
		t.Fatal(err)
	}
	r, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return w, r, sid
}

func readAll(t *testing.T, s *Store, sid session.SessionID) []event.Event {
	t.Helper()
	evs, err := s.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestAReaderSeesWhatAnotherProcessAppends(t *testing.T) {
	w, r, sid := twoStores(t)
	ctx := context.Background()

	// The reader looks first — this is what warms its cache, and what used to freeze it.
	if got := len(readAll(t, r, sid)); got != 1 {
		t.Fatalf("first read returned %d events, want 1", got)
	}

	for i := 0; i < 3; i++ {
		if _, err := w.Append(ctx, sid, mkEvent(t, event.TypePromptSubmitted,
			event.PromptSubmittedData{MessageID: "m", Parts: []session.Part{{Kind: session.PartText, Text: "hi"}}})); err != nil {
			t.Fatal(err)
		}
		want := 2 + i
		got := readAll(t, r, sid)
		if len(got) != want {
			t.Fatalf("after %d appends the reader sees %d events, want %d — the cache is serving a snapshot",
				i+1, len(got), want)
		}
		// Seqs must stay ascending and gapless: a tail appended at the wrong offset shows up here
		// as a duplicate or a hole long before anyone notices a wrong transcript.
		for j, e := range got {
			if e.Seq != int64(j+1) {
				t.Fatalf("event %d has seq %d — the tail was spliced in wrong: %v", j, e.Seq, seqsOf(got))
			}
		}
	}

	// Incremental reads must agree with a cold one over the same directory.
	cold, err := New(w.root)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := seqsOf(readAll(t, r, sid)), seqsOf(readAll(t, cold, sid)); !reflect.DeepEqual(a, b) {
		t.Errorf("the incrementally-updated reader has %v and a cold one has %v", a, b)
	}
}

// fromSeq is how the loop and every poller ask for "only what is new". A stale cache made that
// question answerable with nothing, which is indistinguishable from "no new work" — the failure
// that makes a watcher believe an agent is idle while it works.
func TestIncrementalReadsAnswerFromSeq(t *testing.T) {
	w, r, sid := twoStores(t)
	ctx := context.Background()
	readAll(t, r, sid)

	if _, err := w.Append(ctx, sid,
		mkEvent(t, event.TypeTurnFinished, event.TurnFinishedData{}),
		mkEvent(t, event.TypeTurnFinished, event.TurnFinishedData{}),
	); err != nil {
		t.Fatal(err)
	}
	got, err := r.Read(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Read(fromSeq=1) returned %d events, want the 2 that arrived after seq 1", len(got))
	}
	if got[0].Seq != 2 || got[1].Seq != 3 {
		t.Errorf("got seqs %v, want [2 3]", seqsOf(got))
	}
}

// A log that got SHORTER is a compact or a rewind in the other process. The tail logic cannot help
// there — what the reader holds is not a prefix of what is on disk any more — so it must reload.
func TestAReaderReloadsAfterTheLogIsRewritten(t *testing.T) {
	w, r, sid := twoStores(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := w.Append(ctx, sid, mkEvent(t, event.TypeTurnFinished, event.TurnFinishedData{})); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(readAll(t, r, sid)); got != 5 {
		t.Fatalf("reader sees %d events before the rewind, want 5", got)
	}

	if err := w.Truncate(ctx, sid, 2); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, r, sid)
	if len(got) != 2 {
		t.Fatalf("after a rewind to seq 2 the reader sees %d events, want 2: %v", len(got), seqsOf(got))
	}
}

// A session created after this process indexed the directory is still readable. The index is built
// at New; a dashboard started before the agent it is watching would otherwise get an empty
// transcript with no error — the log is right there on disk.
func TestAReaderFindsASessionCreatedAfterItStarted(t *testing.T) {
	root := t.TempDir()
	r, err := New(root) // the viewer starts FIRST, with nothing to index
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	sid := session.SessionID("s_later")
	if _, err := w.Append(context.Background(), sid,
		mkEvent(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w"}),
		mkEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m"}),
	); err != nil {
		t.Fatal(err)
	}
	if got := len(readAll(t, r, sid)); got != 2 {
		t.Fatalf("the reader sees %d events of a session created after it started, want 2", got)
	}
	// And a session that genuinely does not exist is still nothing, not an error.
	if got := len(readAll(t, r, "s_nowhere")); got != 0 {
		t.Errorf("an unknown session returned %d events", got)
	}
}
