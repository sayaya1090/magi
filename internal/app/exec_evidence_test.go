package app

import "testing"

// The exercise ledger marks an authored runnable file exercised only when an
// EXERCISING (non-inspect) command names it; unexercisedArtifacts lists the rest,
// skipping non-runnable extensions and deletions.
func TestUnexercisedArtifacts(t *testing.T) {
	g := newRunGuard()
	g.recordChange("run.py", "", "print('x')\n")
	g.recordChange("notes.md", "", "docs\n")     // non-runnable ext → never listed
	g.recordChange("gone.sh", "old\n", "")       // emptied/deleted → never listed
	g.recordChange("server.js", "", "serve()\n") // runnable, never run

	g.noteBashExec("cat run.py", false) // inspect-only → not exercise
	if got := g.unexercisedArtifacts(); len(got) != 2 {
		t.Fatalf("want run.py+server.js unexercised, got %v", got)
	}
	g.noteBashExec("python3 run.py --demo", false) // real invocation names run.py
	got := g.unexercisedArtifacts()
	if len(got) != 1 || got[0] != "server.js" {
		t.Fatalf("want only server.js, got %v", got)
	}
	g.noteBashExec("node server.js & sleep 1", false)
	if got := g.unexercisedArtifacts(); len(got) != 0 {
		t.Fatalf("want none, got %v", got)
	}
}

// The ledger requires a WHOLE-token match: running a differently-named file whose basename merely
// CONTAINS the authored file's basename must not mark the authored file exercised (python ax.py is
// not running x.py). Regression for a strings.Contains over-match that silently dropped a
// written-but-never-run artifact off the exec-evidence ledger.
func TestExerciseLedgerWholeTokenMatch(t *testing.T) {
	g := newRunGuard()
	g.recordChange("x.py", "", "core()\n")    // the authored file
	g.recordChange("ax.py", "", "helper()\n") // a different file whose name contains "x.py"

	g.noteBashExec("python3 ax.py", false) // runs ax.py, NOT x.py
	found := map[string]bool{}
	for _, p := range g.unexercisedArtifacts() {
		found[p] = true
	}
	if !found["x.py"] {
		t.Error("x.py must still be unexercised — running ax.py is not running x.py")
	}
	if found["ax.py"] {
		t.Error("ax.py was run and must be exercised")
	}
	// A boundary-delimited mention (path prefix + trailing metachar) does mark it.
	g.noteBashExec("python3 ./x.py; echo done", false)
	for _, p := range g.unexercisedArtifacts() {
		if p == "x.py" {
			t.Error("./x.py is a real mention of x.py — should be exercised now")
		}
	}
}

func TestCmdMentionsFile(t *testing.T) {
	cases := []struct {
		cmd, base string
		want      bool
	}{
		{"python x.py", "x.py", true},
		{"python ax.py", "x.py", false},    // substring of a longer name
		{"./x.py", "x.py", true},           // path-prefixed
		{"dir/x.py --flag", "x.py", true},  // dir-prefixed
		{`run "x.py"`, "x.py", true},       // quoted
		{"cat test.pyc", "test.py", false}, // trailing byte extends the name
		{"x.py", "x.py", true},             // whole string
		{"a.x.py", "x.py", false},          // dotted-prefixed (different file)
	}
	for _, c := range cases {
		if got := cmdMentionsFile(c.cmd, c.base); got != c.want {
			t.Errorf("cmdMentionsFile(%q,%q)=%v want %v", c.cmd, c.base, got, c.want)
		}
	}
}
