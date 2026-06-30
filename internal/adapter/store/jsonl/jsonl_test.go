package jsonl

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

const wd = "/proj"

func created(workdir string, ts time.Time) event.Event {
	d, _ := json.Marshal(event.SessionCreatedData{Workdir: workdir, Agent: "default"})
	return event.Event{Type: event.TypeSessionCreated, TS: ts, Data: d}
}

func fact(t event.Type, ts time.Time) event.Event {
	return event.Event{Type: t, TS: ts}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// append-1, append-2: seq assignment starts at 1 and increases.
func TestAppendSeq(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()

	got, err := s.Append(ctx, "s1", created(wd, ts))
	if err != nil {
		t.Fatalf("append-1: %v", err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("append-1: seq=%v, want [1]", got)
	}

	got, err = s.Append(ctx, "s1",
		fact(event.TypePromptSubmitted, ts),
		fact(event.TypePartAppended, ts),
	)
	if err != nil {
		t.Fatalf("append-2: %v", err)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("append-2: seq=%v, want [2 3]", got)
	}
}

// Read is served from the in-memory cache, and Append keeps a warm cache current —
// a Read after a warming Read still reflects later appends (no stale cache).
func TestReadCacheReflectsAppends(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()

	if _, err := s.Append(ctx, "s1", created(wd, ts)); err != nil {
		t.Fatalf("append-1: %v", err)
	}
	// First Read warms the cache.
	if got, _ := s.Read(ctx, "s1", 0); len(got) != 1 {
		t.Fatalf("warm read: got %d, want 1", len(got))
	}
	// A later append must be visible through the warm cache.
	if _, err := s.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts)); err != nil {
		t.Fatalf("append-2: %v", err)
	}
	got, _ := s.Read(ctx, "s1", 0)
	if len(got) != 2 || got[1].Seq != 2 {
		t.Fatalf("read after append: got %d events (last seq %v), want 2 (seq 2)", len(got), lastSeq(got))
	}
	// The returned slice is a copy — mutating it must not corrupt the cache.
	got[0].Seq = 999
	again, _ := s.Read(ctx, "s1", 0)
	if again[0].Seq != 1 {
		t.Errorf("cache was mutated through a returned slice: seq=%d", again[0].Seq)
	}
}

func lastSeq(evs []event.Event) int64 {
	if len(evs) == 0 {
		return 0
	}
	return evs[len(evs)-1].Seq
}

// append: transient events are rejected.
func TestAppendRejectsTransient(t *testing.T) {
	s := newStore(t)
	_, err := s.Append(context.Background(), "s1", fact(event.TypePartDelta, time.Now()))
	if err == nil {
		t.Fatal("expected error appending transient event, got nil")
	}
}

// append-3: concurrent appends produce unique, gap-free seqs.
func TestAppendConcurrent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()
	if _, err := s.Append(ctx, "s1", created(wd, ts)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const n = 100
	var wg sync.WaitGroup
	seen := make([]int64, 0, n)
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
			if err != nil {
				t.Errorf("concurrent append: %v", err)
				return
			}
			mu.Lock()
			seen = append(seen, got[0])
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Expect seqs 2..101, each exactly once.
	found := make(map[int64]bool)
	for _, v := range seen {
		if found[v] {
			t.Fatalf("duplicate seq %d", v)
		}
		found[v] = true
	}
	for want := int64(2); want <= n+1; want++ {
		if !found[want] {
			t.Fatalf("missing seq %d", want)
		}
	}
}

// read-replay-1, read-replay-2: Read honors fromSeq.
func TestReadFromSeq(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()
	s.Append(ctx, "s1", created(wd, ts))
	s.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts))
	s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	s.Append(ctx, "s1", fact(event.TypeTurnFinished, ts))

	all, err := s.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(all) != 4 || all[0].Seq != 1 || all[3].Seq != 4 {
		t.Fatalf("read-replay-1: got %d events seq[0]=%d seq[last]=%d", len(all), all[0].Seq, all[3].Seq)
	}

	tail, err := s.Read(ctx, "s1", 2)
	if err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if len(tail) != 2 || tail[0].Seq != 3 || tail[1].Seq != 4 {
		t.Fatalf("read-replay-2: got %+v, want seq 3,4", seqsOf(tail))
	}
}

// read-replay-3: data persists across Store reopen.
func TestPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := time.Now()

	s1, _ := New(root)
	s1.Append(ctx, "s1", created(wd, ts))
	s1.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts))
	s1.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	s1.Append(ctx, "s1", fact(event.TypeTurnFinished, ts))

	s2, err := New(root) // reopen
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("read-replay-3: got %d events, want 4", len(got))
	}

	// And new appends continue the seq, not restart.
	out, err := s2.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	if err != nil {
		t.Fatalf("append after reopen: %v", err)
	}
	if out[0] != 5 {
		t.Fatalf("seq after reopen = %d, want 5", out[0])
	}
}

// compact-1: Compact replaces seq<=upToSeq with a snapshot, keeps the rest.
func TestCompact(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()
	s.Append(ctx, "s1", created(wd, ts))
	for i := 0; i < 9; i++ { // total seq now 1..10
		s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	}

	snapData, _ := json.Marshal(event.CompactionData{Summary: "summary", ReplacesUpToSeq: 7})
	snap := event.Event{Type: event.TypeCompaction, TS: ts, Data: snapData}
	if err := s.Compact(ctx, "s1", 7, snap); err != nil {
		t.Fatalf("compact: %v", err)
	}

	got, err := s.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("read after compact: %v", err)
	}
	// Expect: [snapshot(seq=7), seq8, seq9, seq10]
	if len(got) != 4 {
		t.Fatalf("compact-1: got %d events, want 4 (snap+3)", len(got))
	}
	if got[0].Type != event.TypeCompaction {
		t.Errorf("first event = %q, want compaction", got[0].Type)
	}
	if got[1].Seq != 8 || got[2].Seq != 9 || got[3].Seq != 10 {
		t.Errorf("kept seqs = %v, want 8,9,10", seqsOf(got[1:]))
	}

	// Subsequent appends continue from the original seq (Compact leaves s.seqs intact).
	if _, err := s.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts)); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.Read(ctx, "s1", 0); after[len(after)-1].Seq != 11 {
		t.Errorf("append after compact should be seq 11, got %d", after[len(after)-1].Seq)
	}
}

// Compacting "up to 0" is a no-op: there is nothing to compact, and stamping a
// snapshot with Seq 0 would make it invisible to Read(0) (seqs start at 1). The log
// must be left untouched, not silently gain an unreadable snapshot.
func TestCompactUpToZeroIsNoOp(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	ts := time.Now()
	s.Append(ctx, "s1", created(wd, ts))
	s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	before, _ := s.Read(ctx, "s1", 0)

	snap := event.Event{Type: event.TypeCompaction, TS: ts}
	if err := s.Compact(ctx, "s1", 0, snap); err != nil {
		t.Fatalf("compact up to 0 should not error: %v", err)
	}
	after, _ := s.Read(ctx, "s1", 0)
	if len(after) != len(before) {
		t.Fatalf("compact(0) changed the log: %d → %d events", len(before), len(after))
	}
	for _, e := range after {
		if e.Type == event.TypeCompaction {
			t.Error("compact(0) must not insert a snapshot")
		}
	}
}

// list-sessions-1: only the workdir's sessions, newest first.
func TestListSessions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	s.Append(ctx, "s1", created("/proj", t0))
	s.Append(ctx, "s2", created("/proj", t1))
	s.Append(ctx, "s3", created("/other", t0))

	metas, err := s.ListSessions(ctx, "/proj")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("got %d sessions, want 2 (s3 excluded)", len(metas))
	}
	if metas[0].ID != "s2" || metas[1].ID != "s1" {
		t.Errorf("order = [%s %s], want [s2 s1] (newest first)", metas[0].ID, metas[1].ID)
	}
}

// ListSessions populates Title with the first user prompt, collapsed to one line.
func TestListSessionsTitle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)

	s.Append(ctx, "s1", created(wd, t0))
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1",
		Parts:     []session.Part{{Kind: session.PartText, Text: "  refactor the\n  parser please  "}},
	})
	s.Append(ctx, "s1", event.Event{Type: event.TypePromptSubmitted, TS: t0.Add(time.Second), Data: pd})

	metas, err := s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("got %d sessions, want 1", len(metas))
	}
	if metas[0].Title != "refactor the parser please" {
		t.Errorf("Title = %q, want %q", metas[0].Title, "refactor the parser please")
	}
}

// ListSessions hides subagent (child) sessions; ChildSessions returns them.
func TestListSessionsHidesChildren(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)

	s.Append(ctx, "s_parent", created(wd, t0))
	// A child session created by a subagent spawn (carries Parent).
	cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "explore", Parent: "s_parent"})
	s.Append(ctx, "s_child", event.Event{Type: event.TypeSessionCreated, TS: t0.Add(time.Second), Data: cd})

	top, err := s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].ID != "s_parent" {
		t.Fatalf("ListSessions should show only the parent, got %v", top)
	}

	kids, err := s.ChildSessions(ctx, wd, "s_parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].ID != "s_child" || kids[0].Agent != "explore" {
		t.Fatalf("ChildSessions = %v, want [s_child/explore]", kids)
	}
}

func seqsOf(evs []event.Event) []int64 {
	out := make([]int64, len(evs))
	for i, e := range evs {
		out[i] = e.Seq
	}
	return out
}
