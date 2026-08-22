package app

import (
	"strings"
	"testing"
)

// The reminder escalates. The first ask assumes the agent merely forgot the form; from the second
// on that is disproved, and repeating the first ask verbatim is what let the field case loop —
// "if it is not [finished], keep working" is not violated by announcing the same file a fourth time.
func TestDeclareAskNudgeNeverSaysTheSameThingTwice(t *testing.T) {
	const said = "Now I'll write `eval.scm`. Let me write it."
	seen := map[string]bool{}
	for n := 1; n <= declareAskCap; n++ {
		msg := declareAskNudge(n, said)
		if seen[msg] {
			t.Fatalf("ask %d repeated an earlier reminder verbatim", n)
		}
		seen[msg] = true
	}

	first := declareAskNudge(1, said)
	if !strings.Contains(first, "call the `council` tool with `complete: true`") {
		t.Errorf("the first ask must still name the declaration form, got %q", first)
	}
	if strings.Contains(first, said) {
		t.Error("the first ask quotes nothing back: one quiet response is not yet a pattern")
	}

	// The second ask is the lever: the model cannot see that it repeated itself, because each
	// response is locally reasonable. Only its own words, handed back, show the loop.
	second := declareAskNudge(2, said)
	if !strings.Contains(second, said) {
		t.Errorf("the second ask must quote what the agent actually said, got %q", second)
	}
	if !strings.Contains(second, "must contain a tool call") {
		t.Errorf("the second ask must demand an action, not a declaration, got %q", second)
	}

	// And the cap says it is the cap. Cutting the turn off without warning spends the last ask on
	// nothing: the agent never learns that this response is the one that decides.
	last := declareAskNudge(declareAskCap, said)
	if !strings.Contains(last, "last time") || !strings.Contains(last, "lands exactly as it stands") {
		t.Errorf("the final ask must say the turn ends here, got %q", last)
	}

	// A quiet response with no text at all still gets a reminder that reads.
	if q := declareAskNudge(2, "   "); strings.Contains(q, "said:") {
		t.Errorf("an empty last response must not be quoted as if it spoke, got %q", q)
	}
}
