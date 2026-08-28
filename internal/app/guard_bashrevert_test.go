package app

import (
	"strings"
	"testing"
)

// TestBashWritePathsNamesOnlyWhatItCanName: the destination is read out of the command text, so a
// wrong answer would compare the WRONG file's content and retract real progress. Everything whose
// target needs a shell (globs, variables), spans a directory, or lives inside a payload
// (patch/git apply/tar) must yield nothing — the command still gets its epoch bump, it just gets
// no revert check.
func TestBashWritePathsNamesOnlyWhatItCanName(t *testing.T) {
	named := map[string][]string{
		"cp shared_heap.c.bak shared_heap.c":  {"shared_heap.c"},
		"mv a.tmp a.txt":                      {"a.txt"},
		"sed -i 's/a/b/' runtime/major_gc.c":  {"runtime/major_gc.c"},
		"sed -i.bak -e 's/a/b/' x.c":          {"x.c"},
		"perl -i -pe 's/a/b/' y.pl":           {"y.pl"},
		"echo hi > out.txt":                   {"out.txt"},
		"make 2>&1 | tee build.log":           {"build.log"},
		"rm stale.o":                          {"stale.o"},
		"touch marker":                        {"marker"},
		"cat > sol.py <<'EOF'\nprint(1)\nEOF": {"sol.py"},
		"cp a.c b.c && sed -i 's/x/y/' b.c":   {"b.c"},
		"gcc -o prog prog.c > build.log 2>&1": {"build.log"},
		"echo one > a.txt; echo two > b.txt":  {"a.txt", "b.txt"},
	}
	for cmd, want := range named {
		got := bashWritePaths(cmd)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("bashWritePaths(%q) = %v, want %v", cmd, got, want)
		}
	}
	// Unnameable destinations: silence, not a guess.
	for _, cmd := range []string{
		"cp -r src/ dst/",       // a tree, not one file
		"cp a.c b.c dstdir/",    // directory target
		"mv *.c src/",           // glob + directory
		"sed -i 's/a/b/' *.c",   // glob
		"patch -p1 < fix.diff",  // destination is inside the payload
		"git apply fix.patch",   // …same
		"tar xzf bundle.tar.gz", // …same
		"echo x > $OUT",         // needs shell expansion
		"pytest 2>&1",           // fd duplication, no file
		"./run > /dev/null",     // sink
		"grep foo src/",         // reads nothing into a file
		"sed 's/a/b/' x.c",      // no -i: prints, changes nothing
	} {
		if got := bashWritePaths(cmd); len(got) != 0 {
			t.Errorf("bashWritePaths(%q) must name nothing, got %v", cmd, got)
		}
	}
	// The cap keeps one wide command from flooding the per-turn content history.
	var wide []string
	for i := 0; i < bashWriteCap+5; i++ {
		wide = append(wide, "f"+string(rune('a'+i)))
	}
	if got := bashWritePaths("rm " + strings.Join(wide, " ")); len(got) != bashWriteCap {
		t.Errorf("a wide command must cap at %d paths, got %d", bashWriteCap, len(got))
	}
	// redirectsToFile keeps its answers now that it is built on redirectTargets.
	for _, cmd := range []string{"echo x > f", "cat >> f", "make | tee log", "cat <<'EOF'\nx\nEOF"} {
		if !redirectsToFile(cmd) {
			t.Errorf("redirectsToFile(%q) must still be true", cmd)
		}
	}
	for _, cmd := range []string{"pytest 2>&1", "./run >/dev/null", "ls -la"} {
		if redirectsToFile(cmd) {
			t.Errorf("redirectsToFile(%q) must still be false", cmd)
		}
	}
}

// TestBashWritePathsReadsPastTheRedirections: a silenced restore is the commonest revert shape
// there is — an agent that is unsure whether its edit helped writes `cp f.bak f 2>/dev/null` — and
// counting `2>/dev/null` as an argument made it read as a three-operand directory copy, so the one
// command the content check most needed to see was the one it skipped. The same miscount let `rm`
// and `touch` adopt the redirect itself as a written file, which compares a path that cannot exist.
func TestBashWritePathsReadsPastTheRedirections(t *testing.T) {
	named := map[string][]string{
		// The observed shape: silenced restore, tolerated failure, then a look at the result.
		"cp runtime/shared_heap.c.bak runtime/shared_heap.c 2>/dev/null || true; sed -n '1,5p' x": {
			"runtime/shared_heap.c",
		},
		"cp a.bak a.c 2>/dev/null":        {"a.c"},
		"cp a.bak a.c > /dev/null 2>&1":   {"a.c"},
		"mv -f a.c.bak a.c 2>/dev/null":   {"a.c"},
		"rm -f out.txt 2>/dev/null":       {"out.txt"},
		"touch marker 2>/dev/null":        {"marker"},
		"sed -i 's/a/b/' f.c 2>/dev/null": {"f.c"},
		// A redirect whose operator stands alone takes the next token with it, and the destination
		// still arrives through redirectTargets rather than as an operand.
		"cp a.bak a.c 2> /dev/null": {"a.c"},
		"cp a.bak a.c > log 2>&1":   {"log", "a.c"},
	}
	for cmd, want := range named {
		if got := bashWritePaths(cmd); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("bashWritePaths(%q) = %v, want %v", cmd, got, want)
		}
	}
	// What the redirections hid must stay hidden: reading past them may not turn an unnameable
	// destination into a named one.
	for _, cmd := range []string{
		"cp a.c b.c dstdir/ 2>/dev/null", // still a directory target
		"cp -r src/ dst/ 2>/dev/null",    // still a tree
		"mv *.c src/ 2>/dev/null",        // still a glob
	} {
		if got := bashWritePaths(cmd); len(got) != 0 {
			t.Errorf("bashWritePaths(%q) must name nothing, got %v", cmd, got)
		}
	}
}

// TestBashRevertIsNotANewDeliverableVersion: mutated() keys EVERY bash mutation under one slot and
// compares COMMAND TEXT, so a `sed -i` and the `cp f.bak f` that undoes it always read as two
// different changes — the loop is invisible to it no matter how many times it swings. Each swing
// therefore zeroed the progress counters and re-armed the one-shot act-now nudge, which is why the
// nudge fired once and then could never reach its threshold again while the run oscillated to the
// wall clock. The content check is the only thing that can see it, and it already exists for
// write/edit; this pins that a bash mutation goes through it too.
func TestBashRevertIsNotANewDeliverableVersion(t *testing.T) {
	g := newRunGuard(nil)
	const path = "shared_heap.c"

	// v0 → v1: a real new version. It is progress, so the counters restart and the nudge re-arms.
	g.sinceProgress = noProgressNudge
	_, reset := g.noteBashWrite("sed -i 's/old/new/' " + path)
	if !reset {
		t.Fatal("a redirect-less mutation must bump the epoch and reset the counters")
	}
	if warn, regressed := g.noteEdit(path, "v0", "v1"); regressed || warn != "" {
		t.Fatalf("a first, forward change is not a revert (warn=%q)", warn)
	}
	if g.sinceProgress != 0 {
		t.Fatal("a real new version restarts the progress window")
	}

	// …then the restore. The command text differs from the last one, so mutated() resets again —
	// and the content check must hand every one of those resets back.
	g.sinceProgress = noProgressNudge + 2
	_, reset = g.noteBashWrite("cp " + path + ".bak " + path)
	if !reset {
		t.Fatal("the restore is a different command, so mutated() cannot see it is a revert")
	}
	if g.sinceProgress != 0 {
		t.Fatal("precondition: the bump zeroed the window")
	}
	warn, regressed := g.noteEdit(path, "v1", "v0")
	if !regressed {
		t.Fatal("returning the file to a state it already held this turn is a revert")
	}
	if !strings.Contains(warn, "restored a content state") {
		t.Errorf("the agent should be told what it just did, got %q", warn)
	}
	g.retractProgress()

	if g.sinceProgress != noProgressNudge+2 {
		t.Errorf("the progress window must keep climbing across the swing, got %d", g.sinceProgress)
	}
}

// TestBashRewriteToANewStateKeepsItsProgress: the control. A bash mutation that leaves the file in
// a state it has NOT held before is real forward motion and must keep every counter reset — the
// retraction is for reverts only, and over-retracting would stall an agent that is working.
func TestBashRewriteToANewStateKeepsItsProgress(t *testing.T) {
	g := newRunGuard(nil)
	const path = "main.go"
	g.noteEdit(path, "v0", "v1")
	g.sinceProgress = noProgressNudge

	_, reset := g.noteBashWrite("sed -i 's/v1/v2/' " + path)
	if !reset {
		t.Fatal("precondition: the mutation bumped the epoch")
	}
	if _, regressed := g.noteEdit(path, "v1", "v2"); regressed {
		t.Fatal("a state this file has never held is forward progress, not a revert")
	}
	if g.sinceProgress != 0 {
		t.Error("forward progress keeps its counter reset")
	}
}
