package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A memory has no timestamp, so "arrived mid-session" is read off the retrieval: the first match
// set of a session is the baseline — the count pointer already advertises it — and an ID that
// enters the set later came by a side door (engram's sidecar, another companion, a person's
// editor). Those are announced as one-line handles APPENDED to the transcript, never folded into
// anything already sent, which is the same bargain the frozen skill head strikes.

func TestFirstMatchSetIsTheBaselineNotAnAnnouncement(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	a.noteMemoryArrivals("s1", []port.Memory{{ID: "m1", Text: "old lesson"}, {ID: "m2", Text: "older lesson"}})
	if got := a.takeMemoryArrivals("s1", 3); got != nil {
		t.Fatalf("the opening set is what the count pointer covers — announcing it is the pointer again, louder: %v", got)
	}
}

func TestAMemoryEnteringTheSetLaterIsAnnouncedOnce(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	a.noteMemoryArrivals("s1", []port.Memory{{ID: "m1", Text: "old lesson"}})
	a.noteMemoryArrivals("s1", []port.Memory{{ID: "m1", Text: "old lesson"}, {ID: "m2", Text: "the port was held by a zombie server\nsecond line"}})

	got := a.takeMemoryArrivals("s1", 3)
	if len(got) != 1 || !strings.Contains(got[0], "m2") {
		t.Fatalf("exactly the newcomer, by handle: %v", got)
	}
	if strings.Contains(got[0], "\n") {
		t.Fatalf("the note is ONE line however many the memory body has: %q", got[0])
	}
	if again := a.takeMemoryArrivals("s1", 3); again != nil {
		t.Fatalf("announced twice is the transcript growing for nothing: %v", again)
	}
	// Seen is seen, even across another retrieval.
	a.noteMemoryArrivals("s1", []port.Memory{{ID: "m2", Text: "the port was held by a zombie server"}})
	if again := a.takeMemoryArrivals("s1", 3); again != nil {
		t.Fatalf("re-matching an announced memory must not re-announce it: %v", again)
	}
}

func TestArrivalsDrainIsCappedAndKeepsOrder(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	a.noteMemoryArrivals("s1", nil) // baseline: empty store
	a.noteMemoryArrivals("s1", []port.Memory{
		{ID: "m1", Text: "one"}, {ID: "m2", Text: "two"}, {ID: "m3", Text: "three"}, {ID: "m4", Text: "four"},
	})
	first := a.takeMemoryArrivals("s1", 3)
	if len(first) != 3 {
		t.Fatalf("capped at three per step so a busy fleet cannot turn a step into a bulletin board: %v", first)
	}
	rest := a.takeMemoryArrivals("s1", 3)
	if len(rest) != 1 || !strings.Contains(rest[0], "m4") {
		t.Fatalf("the overflow keeps its place for the next step: %v", rest)
	}
}

// The queue is fed from the retrieval the pointer already makes — no extra store call. This pins
// the seam: a second retrieval with a grown result set queues the newcomer.
func TestArrivalsRideTheExistingRetrieval(t *testing.T) {
	exp := &growingExp{}
	a := &App{states: map[session.SessionID]*sessionState{}, cfg: Config{Experience: exp}}
	s := session.Session{ID: "s1"}
	agent := AgentSpec{Tools: []string{"recall_memory"}}

	// Turn 1: baseline.
	_ = a.experiencePointerCached(context.Background(), s.ID, "fix the flaky port", agent.Groups)
	// The store grows; a NEW turn (new query → cache miss) retrieves again.
	exp.grown = true
	_ = a.experiencePointerCached(context.Background(), s.ID, "fix the flaky port test", agent.Groups)

	got := a.takeMemoryArrivals("s1", 3)
	if len(got) != 1 || !strings.Contains(got[0], "m-new") {
		t.Fatalf("the memory that entered the match set should be queued by the retrieval itself: %v", got)
	}
}

type growingExp struct{ grown bool }

func (g *growingExp) Retrieve(_ context.Context, _ string, _ []string) ([]port.Memory, []port.Skill, error) {
	mems := []port.Memory{{ID: "m-old", Text: "was always here"}}
	if g.grown {
		mems = append(mems, port.Memory{ID: "m-new", Text: "landed mid-session"})
	}
	return mems, nil, nil
}
func (g *growingExp) Propose(context.Context, port.Contribution) error { return nil }
