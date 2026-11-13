package jsonl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestLazyIndexLoadsSequenceOnlyForWrittenSession(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, id := range []session.SessionID{"one", "two"} {
		if _, err := s.Append(ctx, id, created(wd, time.Now()), fact(event.TypeTurnFinished, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.seqs) != 0 || len(fresh.cache) != 0 || len(fresh.paths) != 2 {
		t.Fatal("startup parsed unopened bodies")
	}
	seqs, err := fresh.Append(ctx, "one", fact(event.TypeTurnFinished, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if seqs[0] != 3 || len(fresh.seqs) != 1 {
		t.Fatalf("sequence initialization: %v %v", seqs, fresh.seqs)
	}
}

func TestListingCacheTracksExternalChangesAndDoesNotExposeLabels(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	labels, _ := json.Marshal(event.LabelsChangedData{Labels: []string{"before"}})
	if _, err := s.Append(ctx, "one", created(wd, time.Now()), event.Event{Type: event.TypeLabelsChanged, Data: labels}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.listing) != 1 {
		t.Fatal("metadata not cached")
	}
	list[0].Labels[0] = "mutated"
	list, err = s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Labels[0] != "before" {
		t.Fatal("caller mutated metadata cache")
	}
	external, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	labels, _ = json.Marshal(event.LabelsChangedData{Labels: []string{"after"}})
	if _, err = external.Append(ctx, "one", event.Event{Type: event.TypeLabelsChanged, Data: labels}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Labels[0] != "after" {
		t.Fatal("external append invisible")
	}
	path := s.paths["one"]
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Atomically replace with identical size/time but a different inode.
	for i := range raw {
		if i+5 <= len(raw) && string(raw[i:i+5]) == "after" {
			copy(raw[i:i+5], "other")
			break
		}
	}
	if err = os.WriteFile(path+".new", raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path+".new", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Labels[0] != "other" {
		t.Fatal("same-size replacement invisible")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListSessions(ctx, wd)
	if err != nil || len(list) != 0 {
		t.Fatalf("deleted session visible: %v %v", list, err)
	}
}

func BenchmarkLazyHistory(b *testing.B) {
	root := b.TempDir()
	s, err := New(root)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 64; i++ {
		sid := session.SessionID(fmt.Sprintf("session-%d", i))
		raw, _ := json.Marshal(strings.Repeat("x", 256*1024))
		if _, err := s.Append(ctx, sid, created(wd, time.Now()), event.Event{Type: event.TypePartAppended, Data: raw}); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("Open64Logs16MiB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := New(root); err != nil {
				b.Fatal(err)
			}
		}
	})
	if _, err := s.ListSessions(ctx, wd); err != nil {
		b.Fatal(err)
	}
	b.Run("RestartList64Logs16MiB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fresh, err := New(root)
			if err != nil {
				b.Fatal(err)
			}
			if _, err = fresh.ListSessions(ctx, wd); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("WarmList64Logs16MiB", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := s.ListSessions(ctx, wd); err != nil {
				b.Fatal(err)
			}
		}
	})
}
