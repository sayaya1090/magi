package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The safe point has to be a DECISION, not a report: the step that finds nothing running is the step
// that shuts the door. Asking and then acting leaves a turn able to start in between — and the action
// is a restart, so that turn is thrown away by a decision that had just concluded there was none
// (CLIENT_LIFECYCLE §9.3).
func TestHoldForUpdateShutsTheDoorItJustFoundOpen(t *testing.T) {
	a := newTestApp(t)
	release, ok := a.HoldForUpdate()
	if !ok {
		t.Fatal("an idle companion refused the hold")
	}

	// A turn arriving now must not start. Driven through startRun, which is the one door a run
	// comes through — asserting on the flag would measure the flag rather than the rule.
	sid := session.SessionID("s-held")
	a.startRun(context.Background(), sid)
	a.mu.Lock()
	st := a.states[sid]
	started := st != nil && st.cancel != nil
	a.mu.Unlock()
	if started {
		t.Fatal("a turn started while the door was held shut — the restart would throw it away")
	}

	release()
	if _, again := a.HoldForUpdate(); !again {
		t.Error("the door never reopened")
	}
}

// And the other direction: a companion in the middle of something refuses the hold, so the caller
// waits instead of restarting onto it.
func TestHoldForUpdateRefusesWhileSomethingIsRunning(t *testing.T) {
	a := newTestApp(t)
	sid := session.SessionID("s-busy")
	a.mu.Lock()
	st := a.stateLocked(sid)
	_, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	a.mu.Unlock()
	defer cancel()

	if _, ok := a.HoldForUpdate(); ok {
		t.Fatal("it held the door shut while a turn was running — that turn is the one that gets lost")
	}
}

// A meeting round being composed is work the run states deliberately do not cover, and the
// auto-update loop has always waited on it. One definition of busy, not two.
func TestHoldForUpdateWaitsForAMeetingRound(t *testing.T) {
	a := newTestApp(t)
	a.meetingRounds.Add(1)
	if _, ok := a.HoldForUpdate(); ok {
		t.Fatal("a meeting round in composition did not stop the restart")
	}
	a.meetingRounds.Add(-1)
	if _, ok := a.HoldForUpdate(); !ok {
		t.Error("it stayed shut after the round ended")
	}
}

// Two callers cannot both hold: the second would think it had the door when the first is about to
// let go of it.
func TestOnlyOneHoldAtATime(t *testing.T) {
	a := newTestApp(t)
	release, ok := a.HoldForUpdate()
	if !ok {
		t.Fatal(ok)
	}
	if _, second := a.HoldForUpdate(); second {
		t.Fatal("two callers both believe they hold the door shut")
	}
	release()
}

// A meeting round starting the instant after the hold was taken is invisible to it — and the restart
// then throws away exactly the work the hold exists to protect. Both directions go through one lock.
func TestAMeetingRoundCannotBeginUnderTheHold(t *testing.T) {
	a := newTestApp(t)
	release, ok := a.HoldForUpdate()
	if !ok {
		t.Fatal("an idle companion refused the hold")
	}
	if a.beginMeetingRound() {
		t.Fatal("a meeting round began while the door was held shut — the restart discards it")
	}
	release()
	if !a.beginMeetingRound() {
		t.Fatal("rounds never resumed after the hold was released")
	}
	a.endMeetingRound()
}

// And the counter is raised INSIDE the lock, so the hold cannot be taken beside it.
func TestABegunRoundRefusesTheHold(t *testing.T) {
	a := newTestApp(t)
	if !a.beginMeetingRound() {
		t.Fatal(false)
	}
	if _, ok := a.HoldForUpdate(); ok {
		t.Fatal("the door was held shut while a meeting round was being composed")
	}
	a.endMeetingRound()
	if _, ok := a.HoldForUpdate(); !ok {
		t.Error("the door stayed shut after the round ended")
	}
}

// A turn is TWO halves, and the second one is the one that is kept.
//
// ⚠ This measures the DOORS, not beginMeetingRound. The guard above proves the lock refuses; it
// cannot notice a door that never asks it — and that is exactly what happened: MeetingSayIn asked
// while MeetingWriteUp went on incrementing the counter beside the lock. So the fix is only real at
// the call sites, and the call sites are what this pins.
//
// The context is cancelled so a door that does NOT ask fails fast instead of reaching a model: a
// mutant that hangs the build for ten minutes is not a mutant that was caught.
func TestBothHalvesOfAMeetingTurnRefuseUnderTheHold(t *testing.T) {
	a := newTestApp(t)
	release, ok := a.HoldForUpdate()
	if !ok {
		t.Fatal("an idle companion refused the hold")
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const child = session.SessionID("s-meeting")

	doors := map[string]func() error{
		"MeetingSayIn": func() error {
			_, err := a.MeetingSayIn(ctx, child, "design", "topic", "", "", false)
			return err
		},
		"MeetingWriteUp": func() error {
			_, err := a.MeetingWriteUp(ctx, child, "design", "topic", "", "said", nil)
			return err
		},
	}
	for name, knock := range doors {
		err := knock()
		if err == nil {
			t.Errorf("%s ran a round while an update held the door shut", name)
			continue
		}
		if !strings.Contains(err.Error(), "settling an update") {
			t.Errorf("%s did not refuse for the hold — it got as far as %v, so the restart takes the round with it", name, err)
		}
	}
}
