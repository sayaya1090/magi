package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// The members are polled concurrently and the slowest sets the wall clock — a median 87s across
// the recorded runs, every second of it with nothing on screen. So each verdict is announced the
// moment it lands, ahead of the batch the deliberation returns.
//
// It is announced TRANSIENTLY. The record still gets exactly one fact per member, written from
// the returned Deliberation, which is the post-rebuttal set and the only one worth replaying —
// a live preview a later round revises is a display concern, and every surface that reads the log
// keeps counting three verdicts per council.
func TestEachVerdictIsShownWhenItLandsWithoutChangingTheRecord(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Breakdown: council.Breakdown{Done: 3, Voters: 3, Rule: council.RuleMajority},
		Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Done},
			{Member: "Balthasar", Lens: "verification", Decision: council.Done},
			{Member: "Casper", Lens: "completeness", Decision: council.Done},
		},
	}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()

	// Watch the bus, which carries transients; the store carries only facts.
	sub, cancel, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	live := make(chan event.CouncilVerdictData, 16)
	go func() {
		for e := range sub {
			if e.Type != event.TypeCouncilVerdict || e.Seq != 0 { // seq 0 = transient
				continue
			}
			var d event.CouncilVerdictData
			if json.Unmarshal(e.Data, &d) == nil {
				live <- d
			}
		}
	}()

	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "", true); err != nil {
		t.Fatalf("declaring completion: %v", err)
	}

	// The adapter is faked here, so drive the callback the way the real one does: the contract
	// under test is that councilAdvice HANDS one over and it publishes.
	if fc.lastReq.OnVerdict == nil {
		t.Fatal("no OnVerdict was handed to the council, so nothing can be shown before the batch")
	}
	fc.lastReq.OnVerdict(council.Verdict{Member: "Melchior", Lens: "correctness", Decision: council.Done})
	select {
	case got := <-live:
		if got.Member != "Melchior" || got.Decision != string(council.Done) {
			t.Errorf("the live verdict is wrong: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a verdict that landed was not published for the screen")
	}

	// The record is unchanged: one fact per member, no preview among them.
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, e := range evs {
		if e.Type != event.TypeCouncilVerdict {
			continue
		}
		var d event.CouncilVerdictData
		if json.Unmarshal(e.Data, &d) == nil {
			seen[d.Member]++
		}
	}
	if len(seen) != 3 {
		t.Fatalf("want the three members recorded once each, got %v", seen)
	}
	for who, n := range seen {
		if n != 1 {
			t.Errorf("%s was recorded %d times — a preview reached the log", who, n)
		}
	}
}
