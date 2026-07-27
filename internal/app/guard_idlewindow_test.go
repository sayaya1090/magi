package app

import (
	"strings"
	"testing"
)

// The idle nudge told every agent the same two things about its own last few steps — no file
// written, no command run — and only the first was ever measured. A run that had already started
// two builds inside the window was told no command had been run, which is the one kind of error
// that costs the whole message: an agent told a false thing about steps it just took has no reason
// to believe the true half of the same sentence. The parenthetical now comes from the window.
func TestIdleWindowFactsDescribeTheWindowItActuallyHas(t *testing.T) {
	// Nothing authored, nothing run: the original claim, and here it is true.
	g := newRunGuard()
	got := idleWindowFacts(g)
	for _, want := range []string{"no file written", "no command run", "so far this turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("an empty window must say %q, got %q", want, got)
		}
	}

	// Commands ran, nothing was produced: the point stands, but it is a different sentence.
	g.noteBashExec("make world", true)
	got = idleWindowFacts(g)
	if strings.Contains(got, "no command run") {
		t.Errorf("a window with a command in it must not claim none ran, got %q", got)
	}
	if !strings.Contains(got, "1 command") || !strings.Contains(got, "none of them produced or changed one") {
		t.Errorf("what did run must be named, got %q", got)
	}
	g.noteBashExec("pytest -q", true)
	if got = idleWindowFacts(g); !strings.Contains(got, "2 commands") {
		t.Errorf("the count must follow the window, got %q", got)
	}

	// An inspection is not an exercising command and must not be counted as one — otherwise the
	// nudge tells an agent that has only been reading that it has been running things.
	g2 := newRunGuard()
	g2.noteBashExec("cat README.md", true)
	if got := idleWindowFacts(g2); !strings.Contains(got, "no command run") {
		t.Errorf("reading a file is not running one, got %q", got)
	}

	// Once a deliverable exists the window is measured from the last CHANGE, and says so.
	g3 := newRunGuard()
	g3.mutated("main.go", sig(1))
	if got := idleWindowFacts(g3); !strings.Contains(got, "since your last change") {
		t.Errorf("with a deliverable authored the window is since the last change, got %q", got)
	}

	// A nil guard renders nothing rather than a guess.
	if got := idleWindowFacts(nil); got != "" {
		t.Errorf("idleWindowFacts(nil) = %q, want empty", got)
	}
}
