package app

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The backstop used to charge a child for time it spent blocked on a build it could not speed up,
// and lease_verdict short-circuits the whole ladder once it is spent — so the arm written for
// "legitimately waiting on a long external operation" was unreachable exactly when it mattered.
// Live, three sub-planners were handed the identical unit at 15:00 intervals and each was cancelled
// mid-build.
func TestBackstopCreditsTimeSpentWaitingOnSomethingReal(t *testing.T) {
	const base = 5 * time.Minute
	backstop, ceiling := 3*base, 12*base // 15m, 60m

	// Uncredited, the wall is where it always was.
	if got := backstopRemaining(backstop, 0, ceiling, 15*time.Minute); got != 0 {
		t.Errorf("an attempt that waited on nothing still ends at the backstop, got %v", got)
	}
	if got := backstopRemaining(backstop, 0, ceiling, 6*time.Minute); got != 9*time.Minute {
		t.Errorf("remaining before the backstop = %v; want 9m", got)
	}
	// Ten minutes of observed external work pushes the wall out by exactly ten minutes.
	if got := backstopRemaining(backstop, 10*time.Minute, ceiling, 15*time.Minute); got != 10*time.Minute {
		t.Errorf("credited time must come back to the attempt, got %v", got)
	}
	// The ceiling is not pushed by anything: a build still going at an hour is no longer
	// distinguishable from a wedged one.
	if got := backstopRemaining(backstop, 90*time.Minute, ceiling, 55*time.Minute); got != 5*time.Minute {
		t.Errorf("the ceiling must bound a credited attempt, got %v", got)
	}
	if got := backstopRemaining(backstop, 90*time.Minute, ceiling, 60*time.Minute); got > 0 {
		t.Errorf("nothing survives the ceiling, got %v", got)
	}
}

// externalWait is the credit's trigger, and it must answer only for things magi can SEE running.
// Model-side silence — generating, deliberating — is what an absolute ceiling exists to stop and
// must never buy time back.
func TestExternalWaitIsOnlyAboutAProcess(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	const sid = session.SessionID("s_child")

	if a.externalWait(sid) {
		t.Error("an idle child waits on nothing")
	}
	a.enterTool(sid)
	if !a.externalWait(sid) {
		t.Error("a foreground tool call in flight is external work magi can see")
	}
	a.leaveTool(sid)
	if a.externalWait(sid) {
		t.Error("the credit must stop the moment the tool returns")
	}
	// Generating is model-side: it earns a lease extension in the ladder, but never backstop credit.
	a.enterGen(sid)
	if a.externalWait(sid) {
		t.Error("a model mid-response is not an external process")
	}
}
