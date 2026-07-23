package app

import "testing"

// noteCheckFail counts a failing deliverable self-check only when the mutation epoch has advanced
// since the last counted failure (the agent actually edited the deliverable and the same check
// still fails). A repeat with no new edit does not inflate the count — that head-banging is the
// stall path's job. A converging check that passes resets the count via resetCheckChurn.
func TestNoteCheckFailChurn(t *testing.T) {
	g := newRunGuard()

	// First failure with no edit yet (epoch 0): epoch is not > checkFailEpoch(0), so it does not
	// count — a fail before any deliverable edit is not "edited-yet-still-failing" churn.
	if n := g.noteCheckFail(); n != 0 {
		t.Fatalf("fail before any edit must not count, got %d", n)
	}

	// Each real edit bumps the epoch; a failing check after each edit is one churn cycle.
	for want := 1; want <= 3; want++ {
		g.mutated("server.py", sig(want)) // epoch -> want
		if n := g.noteCheckFail(); n != want {
			t.Fatalf("after edit %d, churn = %d, want %d", want, n, want)
		}
	}

	// A repeat failure with NO new edit (epoch unchanged) must not increment.
	if n := g.noteCheckFail(); n != 3 {
		t.Fatalf("repeat fail with no edit must not increment, got %d want 3", n)
	}

	// The check converging (all pass) clears the churn count.
	g.resetCheckChurn()
	if n := g.noteCheckFail(); n != 0 {
		t.Fatalf("after resetCheckChurn a fail with no new edit must be 0, got %d", n)
	}
	// A fresh edit after a reset starts counting from 1 again.
	g.mutated("server.py", sig(99))
	if n := g.noteCheckFail(); n != 1 {
		t.Fatalf("first churn after reset+edit must be 1, got %d", n)
	}
}

// sig returns a distinct mutation signature so each mutated() call is a real change (bumps epoch).
func sig(n int) string { return "v" + string(rune('a'+n%26)) + string(rune('0'+n%10)) }

// The cap flag: unset uses the default, a positive integer overrides, and 0/garbage disables.
func TestCheckChurnCapFlag(t *testing.T) {
	t.Setenv("MAGI_CHECK_CHURN_CAP", "")
	if got := checkChurnCap(); got != defaultCheckChurnCap {
		t.Errorf("unset must use default %d, got %d", defaultCheckChurnCap, got)
	}
	if !checkChurnLandEnabled() {
		t.Error("default must be enabled")
	}
	t.Setenv("MAGI_CHECK_CHURN_CAP", "7")
	if got := checkChurnCap(); got != 7 {
		t.Errorf("override to 7, got %d", got)
	}
	t.Setenv("MAGI_CHECK_CHURN_CAP", "0")
	if got := checkChurnCap(); got != 0 || checkChurnLandEnabled() {
		t.Errorf("0 must disable, got cap=%d enabled=%v", got, checkChurnLandEnabled())
	}
	t.Setenv("MAGI_CHECK_CHURN_CAP", "nonsense")
	if got := checkChurnCap(); got != 0 || checkChurnLandEnabled() {
		t.Errorf("garbage must disable, got cap=%d enabled=%v", got, checkChurnLandEnabled())
	}
	t.Setenv("MAGI_CHECK_CHURN_CAP", "-3")
	if got := checkChurnCap(); got != 0 {
		t.Errorf("negative must disable, got %d", got)
	}
}
