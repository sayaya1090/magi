package companion

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

// words keeps only the terms worth matching on: short ones would turn a filter into a no-op that
// looks like a filter.
func TestWordsDropsTheNoise(t *testing.T) {
	got := words("The API-server, of v2!")
	joined := strings.Join(got, ",")
	if joined != "the,api,server" {
		t.Fatalf("letters and digits, lowered, three runes or more: %q", joined)
	}
	if len(words("a of; !!")) != 0 {
		t.Fatal("nothing but noise matches nothing")
	}
}

// narrow filters a roster by query — but a caller's own row is always kept: whoever asked "who
// does design" and cannot see itself may hand its own work away.
func TestNarrowKeepsYourOwnRowAlways(t *testing.T) {
	list := []fleet.Agent{
		{Name: "design", Role: "draws screens", Socket: "/s/design"},
		{Name: "api", Role: "serves", Socket: "/s/api"},
		{Name: "me", Here: true, Socket: "/s/me"},
	}
	kept, dropped := narrow(list, "design", nil)
	if len(kept) != 2 || dropped != 1 {
		t.Fatalf("the match and the self survive, got %+v (dropped %d)", kept, dropped)
	}
	names := kept[0].Name + "," + kept[1].Name
	if !strings.Contains(names, "design") || !strings.Contains(names, "me") {
		t.Fatalf("kept the wrong rows: %s", names)
	}
	if kept, dropped := narrow(list, "a of", nil); len(kept) != 3 || dropped != 0 {
		t.Fatal("a query of nothing but noise narrows nothing")
	}
	// The learned text is part of the haystack: a companion whose experience mentions the term
	// matches even when its name and role do not.
	kept, _ = narrow(list, "kubernetes", map[string]string{"/s/api": "runs kubernetes upgrades"})
	if len(kept) != 2 || kept[0].Name != "api" {
		t.Fatalf("what a workspace has learned is part of who it is, got %+v", kept)
	}
}

// StateOf answers the waiting asker: over-with-nothing-coming reads differently for a corpse and
// a clean stop, and an unknown session says nothing at all.
func TestStateOfSpeaksForTheDeadHonestly(t *testing.T) {
	list := []fleet.Agent{
		{Name: "gone", Session: "s_dead", State: fleet.Abandoned},
		{Name: "done", Session: "s_done", State: fleet.Stopped},
	}
	if news, over := StateOf(list, "s_dead"); !over || !strings.Contains(news, "unfinished") {
		t.Fatalf("a corpse mid-work: over, and said plainly — got (%q, %v)", news, over)
	}
	if news, over := StateOf(list, "s_done"); !over || !strings.Contains(news, "without finishing") {
		t.Fatalf("a clean stop that never answered is still nothing-coming — got (%q, %v)", news, over)
	}
	// A session no published companion holds: the companion is gone, and gone IS the news — the
	// asker must stop waiting, and the transcript is where any answer would be.
	if news, over := StateOf(list, "s_unknown"); !over || !strings.Contains(news, "no longer published") {
		t.Fatalf("an unpublished session is over-with-nothing-coming, got (%q, %v)", news, over)
	}
	// And a companion mid-ask: not over — the news is who it is waiting on.
	waiting := append(list, fleet.Agent{Name: "stuck", Session: "s_ask",
		State: fleet.Waiting, Asking: "may I run rm?"})
	if news, over := StateOf(waiting, "s_ask"); over || !strings.Contains(news, "blocked waiting") {
		t.Fatalf("blocked-on-a-person is news, not an ending, got (%q, %v)", news, over)
	}
}
