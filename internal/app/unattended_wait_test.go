package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// **"ask" means a person decides, and a job firing at three in the morning has no person.**
//
// Under "ask" the permission wait was unbounded — deliberately, so the terminal's prompt waits as
// long as the person in front of it needs. A scheduled firing has the same policy and no person,
// so it waited forever. And the overlap gate is about the WORKSPACE, so every later firing was
// skipped behind the stuck one: one job wedged the whole schedule, with nothing on any screen
// saying why.
//
// The two halves are measured together on purpose. Bounding everything would take the wait away
// from the person at the terminal — the thing the zero was there to protect — so a test that only
// checked the unattended half would pass on a change that broke the attended one.
func TestAnUnattendedTurnDoesNotWaitForever(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Permission = "ask"
	ctx := context.Background()
	wd := t.TempDir()

	person, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	job, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	// Nobody has spoken in either yet: neither is unattended, and under "ask" neither is bounded.
	if got := a.answerBound(person); got != 0 {
		t.Fatalf("a session with no turn is not unattended, bound was %v", got)
	}

	a.mu.Lock()
	a.stateLocked(person).unattended = isUnattended(event.Actor{Kind: event.ActorUser, ID: "cli"})
	a.stateLocked(job).unattended = isUnattended(cronActor("nightly"))
	a.mu.Unlock()

	// The person at the terminal keeps the wait that "ask" promises.
	if got := a.answerBound(person); got != 0 {
		t.Fatalf("a person's prompt must wait as long as they need, bound was %v", got)
	}
	// The job does not.
	if got := a.answerBound(job); got != unattendedAnswerWait {
		t.Fatalf("a scheduled firing waits forever, bound was %v", got)
	}
}

// A cron actor is what makes a turn unattended, and a person's is not — the predicate is one
// place so the answer cannot depend on which caller is asking.
func TestOnlyAScheduledFiringCountsAsUnattended(t *testing.T) {
	for _, c := range []struct {
		actor event.Actor
		want  bool
	}{
		{cronActor("nightly"), true},
		{event.Actor{Kind: event.ActorUser, ID: "cli"}, false},
		{event.Actor{Kind: event.ActorUser, ID: "tui"}, false},
		{event.Actor{Kind: event.ActorSystem, ID: "loop"}, false},
	} {
		if got := isUnattended(c.actor); got != c.want {
			t.Errorf("actor %q: unattended=%v, want %v", c.actor.ID, got, c.want)
		}
	}
}

// Submit is where the mark is set — the one place a top-level turn begins. Set anywhere else and
// a turn started by a path that forgot it waits forever again.
func TestSubmitMarksAScheduledFiringUnattended(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "check the build"}},
		Actor: cronActor("nightly")}); err != nil {
		t.Fatal(err)
	}
	if !a.isUnattendedSession(sid) {
		t.Fatal("a scheduled firing was submitted and the session is not marked unattended")
	}
}
