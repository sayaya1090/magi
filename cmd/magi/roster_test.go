package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Reading the list never waits for it.
//
// The description this feeds is rebuilt on every step of every turn, and building the list means
// dialling every published socket — usually nothing, occasionally the probe's full timeout per
// companion, and worst exactly when something is already wrong. A read that waited would put that
// on the path of somebody watching the model think.
func TestReadingTheRosterNeverWaitsForIt(t *testing.T) {
	release := make(chan struct{})
	var built int32
	l := newLiveRoster(func() (string, int, error) {
		if atomic.AddInt32(&built, 1) == 1 {
			return "first", 1, nil // startup, where waiting is free
		}
		<-release
		return "second", 1, nil
	})
	l.at = time.Now().Add(-2 * rosterLife) // stale, so the next read starts a refresh

	done := make(chan string, 1)
	go func() { text, _ := l.get(); done <- text }()
	select {
	case got := <-done:
		if got != "first" {
			t.Fatalf("it returned %q instead of what it already had", got)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a read waited for the fleet to be dialled, on the path of every step")
	}
	close(release)
	waitUntil(t, "the refreshed list to land", func() bool { return first(l.get()) == "second" })
}

// Many reads in one window start one refresh, not one each.
//
// A turn takes tens of steps a minute. Without this, a slow fleet becomes a pile of goroutines all
// dialling the same sockets, which is worse than the synchronous read it replaced.
func TestManyReadsStartOneRefresh(t *testing.T) {
	started := make(chan struct{}, 64)
	release := make(chan struct{})
	var n int32
	l := newLiveRoster(func() (string, int, error) {
		if atomic.AddInt32(&n, 1) > 1 { // the first build is startup's, and synchronous
			started <- struct{}{}
			<-release
		}
		return "list", 1, nil
	})
	l.at = time.Now().Add(-2 * rosterLife)
	for i := 0; i < 20; i++ {
		l.get()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("a stale list started no refresh at all")
	}
	select {
	case <-started:
		close(release)
		t.Fatal("a second refresh started for the same stale window")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
}

// A read that failed keeps the last list rather than announcing an empty cluster.
//
// "Nobody else is running" is a real answer and a listing that failed is not it. Collapsed, a
// momentary store error empties the description — which is the frozen-empty failure again, just
// arriving later.
func TestAFailedReadKeepsTheLastList(t *testing.T) {
	var fail atomic.Bool
	l := newLiveRoster(func() (string, int, error) {
		if fail.Load() {
			return "", 0, errors.New("the store is having a moment")
		}
		return "design [core]", 1, nil
	})
	fail.Store(true)
	l.at = time.Now().Add(-2 * rosterLife)
	l.get()
	waitUntil(t, "the failed refresh to finish", func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return !l.busy
	})
	if got, _ := l.get(); !strings.Contains(got, "design") {
		t.Fatalf("a failed listing emptied the roster: %q", got)
	}
}

// first is the text half of a read, for the places a test only cares about that.
func first(s string, _ int) string { return s }

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + what)
}
