package app

import "testing"

// TestIsWaitCommand pins the wait/poll classifier: a command is a wait only when every segment
// is a wait verb or an inspect-only builtin AND at least one segment genuinely waits. Anything
// that also runs real work (pytest, a path-qualified program, curl) is not a pure wait.
func TestIsWaitCommand(t *testing.T) {
	waits := []string{
		`sleep 5`,
		`sleep 300`,
		`ping -c1 host`,
		`ping6 -c1 ::1`,
		`nc -z localhost 22`,
		`until nc -z host 5432; do sleep 5; done`, // readiness poll idiom
		`while ! nc -z db 5432; do sleep 2; done`,
		`ping -c1 vm && echo up`, // a trailing banner is neutral, not disqualifying
		`ping -c1 host; sleep 1`,
	}
	for _, c := range waits {
		if !isWaitCommand(c) {
			t.Errorf("isWaitCommand(%q) = false, want true", c)
		}
	}
	notWaits := []string{
		`pytest`,                       // runs the deliverable
		`sleep 1 && pytest`,            // a real command present
		`timeout 30 pytest`,            // timeout WRAPS a test — real work
		`curl http://host/health`,      // fetches a body — not a pure wait
		`./run`,                        // path-qualified program
		`/usr/bin/sleep 5`,             // path-qualified, even if named sleep
		`echo waiting`,                 // inspect-only, but no genuine wait
		`ls -la`,                       // inspects state, no wait
		``,                             // nothing waited
		`nc -z h 1 && python solve.py`, // wait + real work → not pure
	}
	for _, c := range notWaits {
		if isWaitCommand(c) {
			t.Errorf("isWaitCommand(%q) = true, want false", c)
		}
	}
}

// TestStallIsWait: a no-progress window dominated by wait/poll calls reads as an environment
// wait; a window with real (non-wait) churn does not. noteBashWait counts a poll regardless of
// exit code (a poll to a down host fails while it waits), so a failing-poll stall still trips.
func TestStallIsWait(t *testing.T) {
	// Pure polling: every call a wait → wait-dominated.
	g := newRunGuard()
	for i := 0; i < 6; i++ {
		g.check("bash", nil)            // each call climbs sinceProgress (as execute.go does)
		g.noteBashWait(`ping -c1 host`) // …and counts as a wait
	}
	if !g.stallIsWait() {
		t.Fatalf("a pure-poll window should be wait-dominated (sinceProgress=%d waitSinceMut=%d)",
			g.sinceProgress, g.waitSinceMut)
	}

	// Real churn: varied non-wait calls, no waits → not a wait stall.
	g2 := newRunGuard()
	for i := 0; i < 6; i++ {
		g2.check("bash", nil)
		g2.noteBashWait(`pytest`) // not a wait command → does not count
	}
	if g2.stallIsWait() {
		t.Fatalf("a non-wait churn window must not read as a wait stall (waitSinceMut=%d)", g2.waitSinceMut)
	}

	// A real mutation resets the wait ratio: post-mutation the stale polls no longer dominate.
	g3 := newRunGuard()
	for i := 0; i < 6; i++ {
		g3.check("bash", nil)
		g3.noteBashWait(`sleep 5`)
	}
	g3.mutated("out.txt", "sig") // real progress → resets sinceProgress AND waitSinceMut
	if g3.stallIsWait() {
		t.Fatalf("after a mutation the wait window must reset (sinceProgress=%d waitSinceMut=%d)",
			g3.sinceProgress, g3.waitSinceMut)
	}
}

// TestWaitGuardEnabledFlag: ON by default; only an explicit falsey MAGI_WAIT_GUARD restores the
// unconditional stuck-recovery spawn. Mirrors the soloAudit/orient flag shape.
func TestWaitGuardEnabledFlag(t *testing.T) {
	for _, v := range []string{"", "1", "on", "true", "yes", "ON", "garbage"} {
		t.Setenv("MAGI_WAIT_GUARD", v)
		if !waitGuardEnabled() {
			t.Errorf("MAGI_WAIT_GUARD=%q should enable the wait guard (default on)", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_WAIT_GUARD", v)
		if waitGuardEnabled() {
			t.Errorf("MAGI_WAIT_GUARD=%q should NOT enable", v)
		}
	}
}
