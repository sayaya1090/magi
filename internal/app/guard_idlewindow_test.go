package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// The idle nudge told every agent the same two things about its own last few steps — no file
// written, no command run — and only the first was ever measured. A run that had already started
// two builds inside the window was told no command had been run, which is the one kind of error
// that costs the whole message: an agent told a false thing about steps it just took has no reason
// to believe the true half of the same sentence. The parenthetical now comes from the window.
func TestIdleWindowFactsDescribeTheWindowItActuallyHas(t *testing.T) {
	// Nothing authored, nothing run.
	g := newRunGuard()
	got := idleWindowFacts(g)
	for _, want := range []string{"0 tool calls", "so far this turn", "none of them a build, test, or program run"} {
		if !strings.Contains(got, want) {
			t.Errorf("an empty window must say %q, got %q", want, got)
		}
	}

	// Inspections ran and nothing else: the window is NOT empty, and saying "no command run" here
	// is the same false claim the fixed text used to make — observed live as five `cat`/`ls`/`grep`
	// calls reported as no commands at all.
	g2 := newRunGuard()
	for i := 0; i < 5; i++ {
		g2.check("bash", json.RawMessage(`{"command":"cat `+sig(i)+`"}`))
		g2.noteBashExec("cat README.md", true)
	}
	got = idleWindowFacts(g2)
	if !strings.Contains(got, "5 tool calls") {
		t.Errorf("what ran must be counted, got %q", got)
	}
	if !strings.Contains(got, "none of them a build, test, or program run") {
		t.Errorf("an inspection-only window must be named as one, got %q", got)
	}
	if strings.Contains(got, "no command run") {
		t.Errorf("commands DID run; the window must not deny it, got %q", got)
	}

	// A build ran and still nothing changed: a different window, and a different sentence.
	g3 := newRunGuard()
	g3.check("bash", json.RawMessage(`{"command":"make world"}`))
	g3.noteBashExec("make world", true)
	got = idleWindowFacts(g3)
	if !strings.Contains(got, "including 1 build/test run") {
		t.Errorf("an exercising command must be reported as one, got %q", got)
	}
	if !strings.Contains(got, "still nothing produced or changed") {
		t.Errorf("the point of the nudge must survive, got %q", got)
	}

	// Once a deliverable exists the window is measured from the last CHANGE, and says so.
	g4 := newRunGuard()
	g4.mutated("main.go", sig(1))
	if got := idleWindowFacts(g4); !strings.Contains(got, "since your last change") {
		t.Errorf("with a deliverable authored the window is since the last change, got %q", got)
	}

	// A nil guard renders nothing rather than a guess.
	if got := idleWindowFacts(nil); got != "" {
		t.Errorf("idleWindowFacts(nil) = %q, want empty", got)
	}
}
