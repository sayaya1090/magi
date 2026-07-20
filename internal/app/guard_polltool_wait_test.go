package app

import (
	"encoding/json"
	"testing"
)

// A background-job poll spiral (repeated bash_output on a running build) must read as an
// ENVIRONMENT WAIT, so the stuck-recovery is suppressed for the "repeat" kind — the fix that
// lets MAGI_STUCK_DECOMPOSE default ON without reopening the compile-compcert regression.
// Before this, the poll hard-blocked and blocked calls returned before the post-execute wait
// accounting, so waitSinceMut froze while sinceProgress climbed and stallIsWait() read false.
func TestPollToolCountsAsWait(t *testing.T) {
	g := newRunGuard()
	poll := json.RawMessage(`{"id":"build-1"}`)
	// Poll the same background job repeatedly — after repeatLimit it hard-blocks, and those
	// blocked polls must STILL count toward the wait ratio (they run through check()).
	for i := 0; i < 6; i++ {
		g.check("bash_output", poll)
	}
	if !g.stallIsWait() {
		t.Fatalf("a pure bash_output poll spiral must read as an environment wait "+
			"(sinceProgress=%d waitSinceMut=%d)", g.sinceProgress, g.waitSinceMut)
	}
}

// wait_for is an explicit block-until — also an environment wait.
func TestWaitForCountsAsWait(t *testing.T) {
	g := newRunGuard()
	g.check("wait_for", json.RawMessage(`{"condition":"file exists"}`))
	g.check("wait_for", json.RawMessage(`{"condition":"file exists"}`))
	if !g.stallIsWait() {
		t.Fatal("wait_for calls must count toward the environment-wait ratio")
	}
}

// A genuine fixation — repeated reads with no polling — must NOT read as a wait, so the
// decomposing recovery still fires on it (the fix-ocaml-gc / heap-crash search-loop case).
func TestReadLoopIsNotWait(t *testing.T) {
	g := newRunGuard()
	rd := json.RawMessage(`{"path":"/app/x.c"}`)
	for i := 0; i < 6; i++ {
		g.check("read", rd)
	}
	if g.stallIsWait() {
		t.Fatal("a read/search loop is a fixation, not an environment wait — recovery must NOT be suppressed")
	}
}

// A window with real work between polls must not trip the wait suppression: the ratio needs a
// poll-DOMINATED window, so occasional bash_output amid varied work stays below the half mark.
func TestMixedWindowNotWait(t *testing.T) {
	g := newRunGuard()
	g.check("bash_output", json.RawMessage(`{"id":"j"}`)) // 1 wait
	g.check("read", json.RawMessage(`{"path":"a"}`))
	g.check("bash", json.RawMessage(`{"command":"grep x a"}`))
	g.check("read", json.RawMessage(`{"path":"b"}`))
	g.check("grep", json.RawMessage(`{"pattern":"y"}`)) // sinceProgress=5, waitSinceMut=1
	if g.stallIsWait() {
		t.Fatal("a work-dominated window (one poll among real actions) must not read as a wait")
	}
}
