package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The scratch exists for exactly one turn, which is the whole design: anything that has to outlive
// the turn is a deliverable and belongs in the workspace. That boundary is what removes the need
// for a quota, a rotation policy, or a rule about when a scratch file is stale.
func TestTurnScratchLivesExactlyAsLongAsTheTurn(t *testing.T) {
	sc := newTurnScratch()
	if sc == nil {
		t.Skip("no temp dir available")
	}
	for _, d := range []string{sc.root, sc.logsDir(), sc.tmpDir()} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("%s is not a directory: %v", d, err)
		}
	}
	// The two halves are separate so an `ls` of the agent's temp dir is not a pile of magi's logs.
	if sc.logsDir() == sc.tmpDir() {
		t.Error("captured output and the command's TMPDIR must not be the same directory")
	}
	if !strings.HasPrefix(sc.logsDir(), sc.root) || !strings.HasPrefix(sc.tmpDir(), sc.root) {
		t.Error("both must sit under one root, so removal is one call")
	}
	// It must not be inside a workspace: a grader that diffs or globs the deliverable tree would
	// see magi's logs as junk the agent produced.
	if !strings.HasPrefix(sc.root, os.TempDir()) {
		t.Errorf("scratch must live in the temp dir, got %s", sc.root)
	}

	if err := os.WriteFile(filepath.Join(sc.tmpDir(), "work.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc.remove()
	if _, err := os.Stat(sc.root); !os.IsNotExist(err) {
		t.Errorf("the turn ended; the whole directory must be gone, got %v", err)
	}

	// A nil scratch reads as "no scratch" rather than panicking, so every caller can pass it
	// straight through and get the pre-scratch behavior without branching.
	var none *turnScratch
	if none.logsDir() != "" || none.tmpDir() != "" {
		t.Error("a nil scratch must report empty paths")
	}
	none.remove()
}

// A child works inside the TURN's scratch, not one of its own: its build log is something the
// parent and its siblings read, and a per-child directory would delete that at the child's exit.
func TestScratchIsInheritedAndOwnedByTheTurn(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	sc := newTurnScratch()
	if sc == nil {
		t.Skip("no temp dir available")
	}
	defer sc.remove()

	const parent, child = session.SessionID("s_parent"), session.SessionID("s_child")
	a.setScratch(parent, sc)
	a.setScratch(child, a.scratchFor(parent)) // what spawn does
	if a.scratchFor(child) != sc {
		t.Fatal("a child must run inside the turn's scratch, not a copy")
	}
	if a.scratchFor(child).logsDir() != a.scratchFor(parent).logsDir() {
		t.Error("parent and child must read and write the same directory")
	}
	// A session that was never given one has none, and asking is safe.
	if a.scratchFor("s_unknown") != nil {
		t.Error("an unknown session has no scratch")
	}
}

// A turn removes its own directory, so an orphan means the process died without unwinding. Only
// those are swept, and only when old enough that no live turn could still own them.
func TestSweepReclaimsOnlyOrphans(t *testing.T) {
	base := os.TempDir()
	old := filepath.Join(base, "magi-turn-sweeptest-old")
	fresh := filepath.Join(base, "magi-turn-sweeptest-fresh")
	other := filepath.Join(base, "magi-cp-sweeptest-notours")
	for _, d := range []string{old, fresh, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(d)
	}
	stale := time.Now().Add(-staleScratchAge - time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Skip("cannot age a directory here")
	}
	if err := os.Chtimes(other, stale, stale); err != nil {
		t.Skip("cannot age a directory here")
	}

	sweepStaleScratch(fresh)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an aged orphan must be reclaimed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the live turn's own directory must survive its own sweep")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("only magi-turn-* is ours to remove")
	}
}

// A check MAY read this turn's scratch, and the rule that said otherwise was backwards. It reasoned
// that a scratch file cannot be re-run in a later turn — but a check cannot either: deliverableChecks
// is cleared at every new top-level turn, so the two have exactly the same lifetime. And the check
// contract obliges the worker to redirect the genuine output to the path the item names, so a
// scratch it may not name leaves one place for that file: the workspace, which is the deliverable.
// The rule would have used magi's own contract to push evidence into the tree being graded.
func TestCheckMayReadThisTurnsScratchButNotADeadOne(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	sc := newTurnScratch()
	if sc == nil {
		t.Skip("no temp dir available")
	}
	defer sc.remove()
	const sid = session.SessionID("s_main")
	a.setScratch(sid, sc)

	// This turn's scratch is exactly where check evidence belongs.
	for _, src := range []string{
		filepath.Join(sc.tmpDir(), "result.txt"),
		filepath.Join(sc.logsDir(), "magi-bash-1.log"),
		filepath.Join(sc.root, "a", "..", "b.txt"),
	} {
		if why := a.scratchSourceRefusal(sid, src); why != "" {
			t.Errorf("a check reading %s must be allowed, got %q", src, why)
		}
	}
	// A turn that is over took its directory with it: nothing there to read, ever.
	if why := a.scratchSourceRefusal(sid, "/tmp/magi-turn-999/logs/x.log"); !strings.Contains(why, "a turn that is over") {
		t.Errorf("a dead turn's scratch must be refused by name, got %q", why)
	}
	// With no live scratch at all, any turn directory is a dead one.
	if why := a.scratchSourceRefusal("s_other", filepath.Join(sc.tmpDir(), "x")); why == "" {
		t.Error("a session with no scratch cannot be reading a live one")
	}
	// Everything else is none of magi's business: the workspace, and a /tmp file the agent chose
	// itself, which lives as long as the machine keeps it.
	for _, src := range []string{"/app/build.log", "build/out.txt", "/tmp/my-own-run.log", ""} {
		if why := a.scratchSourceRefusal(sid, src); why != "" {
			t.Errorf("%q must be allowed, got %q", src, why)
		}
	}
}
