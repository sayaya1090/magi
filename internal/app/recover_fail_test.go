package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

var errInjected = errors.New("injected: the disk refused the write")

// refusingStore fails the appends refuse matches, and passes everything else through.
type refusingStore struct {
	port.Store
	mu     sync.Mutex
	refuse func(event.Event) bool
}

func (r *refusingStore) Append(ctx context.Context, s session.SessionID, evs ...event.Event) ([]int64, error) {
	r.mu.Lock()
	f := r.refuse
	r.mu.Unlock()
	for _, e := range evs {
		if f != nil && f(e) {
			return nil, errInjected
		}
	}
	return r.Store.Append(ctx, s, evs...)
}

func (r *refusingStore) allow() { r.mu.Lock(); r.refuse = nil; r.mu.Unlock() }

func byRecovery(typ event.Type) func(event.Event) bool {
	return func(e event.Event) bool { return e.Type == typ && e.Actor == recoveryActor }
}

func countByRecovery(evs []event.Event, typ event.Type) int {
	n := 0
	for _, e := range evs {
		if byRecovery(typ)(e) {
			n++
		}
	}
	return n
}

// A result recovery could not save leaves the turn OPEN: closing it anyway would take it off the
// next start's list with a call that has no result, for good. The next start finishes the job, and
// every call ends up with exactly one result.
// SPEC F-LOOP-INTERRUPT loop-int-4.
func TestRecoveryThatCannotSaveAResultLeavesTheTurnOpen(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t, "c_write")
	rs := &refusingStore{Store: store, refuse: byRecovery(event.TypePartAppended)}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))

	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || !errors.Is(got[0].Err, errInjected) {
		t.Fatalf("the failed save must be reported; got %+v", got)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if n := countByRecovery(evs, event.TypeTurnFinished) + countByRecovery(evs, event.TypeError); n != 0 {
		t.Fatalf("recovery closed a turn whose results it could not save: types=%v", typesOf(evs))
	}

	rs.allow()
	if again := a.RecoverInterrupted(ctx, wd); len(again) != 1 || again[0].Err != nil {
		t.Fatalf("the next start should settle it; got %+v", again)
	}
	evs, _ = store.Read(ctx, sid, 0)
	for _, c := range []string{"c_write", "c_read"} {
		n := 0
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == c {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d results, want exactly 1; types=%v", c, n, typesOf(evs))
		}
	}
	if countByRecovery(evs, event.TypeTurnFinished) != 1 {
		t.Fatalf("the turn should close once; types=%v", typesOf(evs))
	}
}

// A log written before magi recorded tool starts cannot tell "never started" from "ran and died
// before its result was saved". Recovery must not claim the first: an unanswered call there may
// have run, and the turn goes to the person.
func TestRecoveryOfALegacyLogTreatsUnansweredCallsAsUncertain(t *testing.T) {
	ctx := context.Background()
	a, store, sid, wd := crashedLogAs(t, true)
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].Resumable || len(got[0].NotRun) != 0 || strings.Join(got[0].Uncertain, ",") != "c_write,c_read" {
		t.Fatalf("want both calls uncertain and the turn not resumable; got %+v", got)
	}
	evs, _ := store.Read(ctx, sid, 0)
	for c, r := range resultsByCall(t, evs) {
		if strings.Contains(string(r.Content), "never started") || !strings.Contains(string(r.Content), "may have run") {
			t.Fatalf("%s: a legacy call must not be called never-started: %s", c, r.Content)
		}
	}
	if _, ok := a.ResumableTurnOf(ctx, sid); ok {
		t.Fatal("a legacy turn with unanswered calls must not read as resumable")
	}
}

// The interruption saved but the turn's end refused: the next start must notice the end is missing
// and write it — once — rather than reading the error as the ending and leaving consumers that wait
// for turn.finished waiting forever.
func TestRecoveryWritesAMissingTurnEndOnTheNextStart(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t)
	rs := &refusingStore{Store: store, refuse: byRecovery(event.TypeTurnFinished)}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	if got := a.RecoverInterrupted(ctx, wd); len(got) != 1 || !errors.Is(got[0].Err, errInjected) {
		t.Fatalf("the refused end must be reported; got %+v", got)
	}
	rs.allow()
	if got := a.RecoverInterrupted(ctx, wd); len(got) != 1 || got[0].Err != nil {
		t.Fatalf("the next start should write the missing end; got %+v", got)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if countByRecovery(evs, event.TypeTurnFinished) != 1 || countByRecovery(evs, event.TypeError) != 1 {
		t.Fatalf("want one interruption and one end; types=%v", typesOf(evs))
	}
	if got := a.RecoverInterrupted(ctx, wd); len(got) != 0 {
		t.Fatalf("a settled turn must not be recovered again: %+v", got)
	}
	if evs2, _ := store.Read(ctx, sid, 0); len(evs2) != len(evs) {
		t.Fatalf("third pass wrote %d events", len(evs2)-len(evs))
	}
}

// A call recovery answered "may have run" stays uncertain on every later pass. The first pass saved
// that answer and then could not save why the turn stopped; the next start used to count the call
// as answered like any finished one, find nothing uncertain and close the turn as resumable — a
// "continue" would then carry on past a call nobody checked.
func TestReRecoveryKeepsACallItAnsweredMayHaveRunUncertain(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t, "c_write")
	rs := &refusingStore{Store: store, refuse: byRecovery(event.TypeError)}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))

	if got := a.RecoverInterrupted(ctx, wd); len(got) != 1 || !errors.Is(got[0].Err, errInjected) {
		t.Fatalf("the first pass should fail on the error event; got %+v", got)
	}
	rs.allow()
	again := a.RecoverInterrupted(ctx, wd)
	if len(again) != 1 || again[0].Err != nil {
		t.Fatalf("the next start should settle it; got %+v", again)
	}
	if again[0].Resumable || len(again[0].Uncertain) != 1 || again[0].Uncertain[0] != "c_write" {
		t.Fatalf("c_write may have run and must stay uncertain, not resumable; got %+v", again[0])
	}
	evs, _ := store.Read(ctx, sid, 0)
	var resumable []bool
	for _, e := range evs {
		if e.Type == event.TypeError && e.Actor == recoveryActor {
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil {
				resumable = append(resumable, d.Resumable)
			}
		}
	}
	if len(resumable) != 1 || resumable[0] {
		t.Fatalf("want one interruption, not resumable; got %v", resumable)
	}
	if n := countByRecovery(evs, event.TypeTurnFinished); n != 1 {
		t.Fatalf("want one turn end, got %d", n)
	}
	for _, c := range []string{"c_write", "c_read"} {
		n := 0
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == c {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s has %d results, want exactly 1", c, n)
		}
	}
}
