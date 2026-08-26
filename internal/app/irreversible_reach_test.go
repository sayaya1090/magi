package app

import (
	"path/filepath"
	"testing"
)

// The gate refuses what it cannot restore, on the reading that a path outside the tree belongs to
// somebody else. Three kinds of path are outside and are still nobody else's; measured over the
// 25 refusals of the 2026-08-26 sweep, they were 21 of them.
// madeBy answers the way the run's own record does — containment included.
func madeBy(paths ...string) func(string) bool {
	g := &runGuard{}
	for _, p := range paths {
		g.noteCreated(p)
	}
	return g.didCreate
}

func TestReachLetsThroughWhatIsNobodyElses(t *testing.T) {
	wd := "/app"
	for _, tc := range []struct {
		name, cmd string
		mine      func(string) bool
	}{
		{"scratch under /tmp", "rm -rf /tmp/testenv", nil},
		{"scratch under /var/tmp", "rm -rf /var/tmp/build", nil},
		{"a file in scratch", "rm -rf /tmp/solve.sh", nil},
		{"a relative glob cannot leave the cwd", "rm -rf *.gcov", nil},
		{"so can an anchored one", "rm -rf char_*.png", nil},
		{"a path this run made", "rm -rf /app/dclm_backup_1787711029",
			madeBy("/app/dclm_backup_1787711029")},
		{"or one inside a directory it made", "rm -rf /srv/out/frames", madeBy("/srv/out")},
	} {
		if why, yes := needsCouncilBeforeRunning(wd, tc.cmd, tc.mine); yes {
			t.Errorf("%s: gated %q as %q", tc.name, tc.cmd, why)
		}
	}
}

// What the gate is for is untouched.
func TestReachStillHoldsWhatItShould(t *testing.T) {
	wd := "/app"
	for _, tc := range []struct{ name, cmd string }{
		{"somebody else's tree", "rm -rf /server"},
		{"a file in it", "rm -rf /server/index.html"},
		{"the temp area itself, not one thing in it", "rm -rf /tmp"},
		{"a glob that can climb out", "rm -rf ../*"},
		{"a glob with a separator", "rm -rf /etc/*.conf"},
		{"an unresolvable variable", "rm -rf $TARGET"},
		{"a force-push", "git push --force origin main"},
		{"a raw device", "dd if=/dev/zero of=/dev/sda"},
	} {
		if _, yes := needsCouncilBeforeRunning(wd, tc.cmd, nil); !yes {
			t.Errorf("%s: let %q through", tc.name, tc.cmd)
		}
	}
}

// A path the run made is its own however deep it sits; a sibling of it is not.
func TestDidCreateCoversWhatItContains(t *testing.T) {
	g := &runGuard{}
	g.noteCreated("/tmp/work")
	for _, p := range []string{"/tmp/work", "/tmp/work/out.txt", "/tmp/work/a/b/c"} {
		if !g.didCreate(p) {
			t.Errorf("didCreate(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"/tmp/worker", "/tmp", "/tmp/other"} {
		if g.didCreate(p) {
			t.Errorf("didCreate(%q) = true, want false", p)
		}
	}
	if (&runGuard{}).didCreate("/tmp/work") {
		t.Error("an empty guard vouched for a path it never saw")
	}
}

// The destination of a recursive two-operand copy is nameable, and naming it is what lets the
// gate recognise a backup as the run's own.
func TestRecursiveCopyNamesItsDestination(t *testing.T) {
	got := bashWritePaths("cd /app && cp -r dclm dclm_backup_1787711029")
	found := false
	for _, p := range got {
		if p == "dclm_backup_1787711029" {
			found = true
		}
	}
	if !found {
		t.Errorf("bashWritePaths did not name the copy's destination: %v", got)
	}
}

func TestScratchRootsHonourTMPDIR(t *testing.T) {
	t.Setenv("TMPDIR", "/custom/tmp")
	if !isScratchPath("/app", "/custom/tmp/thing") {
		t.Error("TMPDIR is not being read")
	}
	if isScratchPath("/app", filepath.Clean("/custom/tmp")) {
		t.Error("the temp root itself must stay gated")
	}
}
