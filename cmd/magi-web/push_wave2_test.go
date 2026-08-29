package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/webpush"
)

func freshPush(t *testing.T) *pushState {
	t.Helper()
	return &pushState{
		file:    filepath.Join(t.TempDir(), "subs.json"),
		subs:    map[string]webpush.Subscription{},
		who:     map[string]string{},
		agent:   map[string]string{},
		addedAt: map[string]string{},
		was:     map[string]fleet.State{},
		told:    map[string]bool{},
	}
}

// The production tick order, pinned through the one function the loop and this test now share:
// settled reads BEFORE the waiting-edge pass updates `was`. Reversed — as the loop shipped — every
// companion compares against itself and the answered-notification never fires; the old unit test
// called the two halves in the right order and stayed green over a feature that had never worked.
func TestPushEdgesSettleFiresOnTheProductionPath(t *testing.T) {
	p := freshPush(t)
	work := []fleet.Agent{{Socket: "/s/a", Name: "a", State: fleet.Working}}
	idle := []fleet.Agent{{Socket: "/s/a", Name: "a", State: fleet.Idle}}
	if news, settled := pushEdges(p, work); len(news) != 0 || len(settled) != 0 {
		t.Fatalf("tick 1 records, announces nothing: (%v, %v)", news, settled)
	}
	_, settled := pushEdges(p, idle)
	if len(settled) != 1 || settled[0].Name != "a" {
		t.Fatalf("working→idle is the settle edge, and it must fire: %v", settled)
	}
	waiting := []fleet.Agent{{Socket: "/s/a", Name: "a", State: fleet.Waiting}}
	news, _ := pushEdges(p, waiting)
	if len(news) != 1 {
		t.Fatalf("idle→waiting is the news edge: %v", news)
	}
}

// A save triggered by one endpoint's change keeps what the others said about themselves — the
// Gone cleanup used to rewrite Added to now and wipe Agent on every surviving row.
func TestSaveKeepsTheOtherRowsWhole(t *testing.T) {
	p := freshPush(t)
	p.subs["e1"] = webpush.Subscription{Endpoint: "e1"}
	p.agent["e1"], p.addedAt["e1"], p.who["e1"] = "iPhone Safari", "2026-08-01T00:00:00Z", "kim"
	p.subs["e2"] = webpush.Subscription{Endpoint: "e2"}
	p.agent["e2"], p.addedAt["e2"] = "Desktop Chrome", "2026-08-02T00:00:00Z"
	p.saveLocked()

	// e2 goes Gone; the save that follows must not touch e1's story.
	delete(p.subs, "e2")
	delete(p.agent, "e2")
	delete(p.addedAt, "e2")
	p.saveLocked()

	raw, err := os.ReadFile(p.file)
	if err != nil {
		t.Fatal(err)
	}
	var got []storedSub
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != "iPhone Safari" || got[0].Added != "2026-08-01T00:00:00Z" ||
		got[0].Who != "kim" {
		t.Fatalf("the surviving row lost its story: %+v", got)
	}
	if strings.Contains(string(raw), "Desktop") {
		t.Fatal("the gone row is gone")
	}
}

// The stale window serves what it has: with a fetch already in flight, a viewer gets the cached
// list (or one empty frame on the very first fetch) instead of launching a fan-out of its own.
func TestPeerFleetServesStaleWhileOneFetches(t *testing.T) {
	s := &server{peers: []peer{{}}}
	s.peerAt.fetching = true
	s.peerAt.done = true
	s.peerAt.list = []fleet.Agent{{Name: "far"}}
	if got := s.peerFleet(t.Context()); len(got) != 1 || got[0].Name != "far" {
		t.Fatalf("stale beats a stampede: %v", got)
	}
	s.peerAt.done = false
	s.peerAt.list = nil
	if got := s.peerFleet(t.Context()); got != nil {
		t.Fatalf("first fetch in flight: one empty frame, got %v", got)
	}
}
