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

// One rule for replace and remove, with the empty owner carved out: a row subscribed before the
// console had people hears nothing and its documented cure is re-subscribing — a guard that held
// the empty owner locked that door for good (the review's lockout finding).
func TestMayTouchSubOneRuleBothVerbs(t *testing.T) {
	for _, c := range []struct {
		owner string
		held  bool
		who   string
		conf  bool
		want  bool
	}{
		{"alice", true, "alice", true, true}, // your own
		{"alice", true, "bob", true, false},  // somebody else's
		{"", true, "bob", true, true},        // legacy empty owner: re-subscribe must work
		{"", false, "bob", true, true},       // unheld endpoint: nothing to protect
		{"alice", true, "bob", false, true},  // no people configured: no scopes to defend
		{"alice", true, "", true, false},     // the unnamed caller has no claim on a named row
		{"", true, "", true, true},           // unnamed on unnamed: nothing anybody could lose
	} {
		if got := mayTouchSub(c.owner, c.held, c.who, c.conf); got != c.want {
			t.Errorf("mayTouchSub(%q,%v,%q,%v) = %v, want %v", c.owner, c.held, c.who, c.conf, got, c.want)
		}
	}
}

// A cross-machine label is prose, not a name; the scope check gets the name back.
func TestCompanionOfLabelCutsTheMachineSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"api on deskB": "api",
		"api":          "api",
		" on deskB":    " on deskB", // a label that BEGINS with the mark is not a name plus suffix
	} {
		if got := companionOfLabel(in); got != want {
			t.Errorf("companionOfLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// The qualified scope form hears a remote asker: "deskB/api" is the documented way to name a
// same-named companion across machines, and the answer notification's check now runs the
// asker's name under its host as the peer.
func TestQualifiedScopeHearsARemoteAsker(t *testing.T) {
	s := withPolicy(t, `
[people."boss@corp.com"]
role = "operator"

[people."kim@corp.com"]
role = "responder"
companions = ["deskB/api", "docs"]
`)
	p := &pushState{
		subs: map[string]webpush.Subscription{"https://push.example/kim": {Endpoint: "https://push.example/kim"}},
		who:  map[string]string{"https://push.example/kim": "kim@corp.com"},
	}
	if got := p.mayHearPairs(s.policy, splitPair("api on deskB"), hearPair{name: "docs"}); len(got) != 1 {
		t.Fatalf("the deskB/api + docs subscriber must hear an 'api on deskB' → docs answer, got %d", len(got))
	}
	if got := p.mayHearPairs(s.policy, splitPair("api on deskC"), hearPair{name: "docs"}); len(got) != 0 {
		t.Fatalf("deskC is not deskB; the qualified scope must not widen, got %d", len(got))
	}
}
