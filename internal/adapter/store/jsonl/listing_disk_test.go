package jsonl

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSavedListingSurvivesStoreRestartAndRejectsCorruption(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Append(ctx, "one", created(wd, time.Now())); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListSessions(ctx, wd)
	if err != nil {
		t.Fatal(err)
	}
	path := s.paths["one"]
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if listingIdentity(info) == "" {
		t.Skip("persistent identity unavailable on platform")
	}
	key := wd + "\x00" + path
	fresh, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	cached, ok := fresh.loadListing(key, wd, "one", info)
	if !ok || cached.ID != first[0].ID {
		t.Fatal("restart lost saved metadata")
	}
	if err = os.WriteFile(s.listingPath(key), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := fresh.ListSessions(ctx, wd)
	if err != nil || len(list) != 1 {
		t.Fatalf("cache corruption broke listing: %v %v", list, err)
	}
	if _, ok = fresh.loadListing(key, wd, "one", info); !ok {
		t.Fatal("corrupt cache not rebuilt")
	}
	// An unavailable cache directory must not hide valid source sessions.
	if err = os.RemoveAll(filepath.Join(s.root, "listing-cache")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.root, "listing-cache"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err = New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	list, err = fresh.ListSessions(ctx, wd)
	if err != nil || len(list) != 1 {
		t.Fatalf("cache unavailable: %v %v", list, err)
	}
}

func TestSavedListingRejectsInPlaceEditWithRestoredTime(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Append(ctx, "one", created("/proj", time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListSessions(ctx, "/proj"); err != nil {
		t.Fatal(err)
	}
	path := s.paths["one"]
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if listingIdentity(info) == "" {
		t.Skip("persistent identity unavailable on platform")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("default"), []byte("changed"), 1)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions(ctx, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Agent != "changed" {
		t.Fatal("in-memory cache ignored same-size in-place edit")
	}
	fresh, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	list, err = fresh.ListSessions(ctx, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Agent != "changed" {
		t.Fatal("disk cache ignored in-place edit")
	}
}
