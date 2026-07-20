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
