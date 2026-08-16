package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A stale snapshot must not survive beside its replacement. Observed live (the itm→item rename,
// 2026-08-16, /tmp/mx18): the work was correct before the first completion declaration, but the
// evidence block OPENED with a mid-rename read of the file — and all three council members cited
// its long-fixed lines to reject three declarations in a row, one of them while writing down the
// contradiction with the fresh grep in the same block. The council trusted the first big snapshot
// it saw. So the same call asked again keeps only its newest output; the older occurrence stays as
// a one-line stub that names itself superseded and carries no content to anchor on.
func TestARepeatedCallsOlderResultIsSuperseded(t *testing.T) {
	evs := []event.Event{
		evPromptUser(),
		evCall("c1", "read", map[string]any{"path": "inventory.py"}),
		evResult("c1", "def remove_item(itm, stock):\n    del stock[itm]", false),
		evCall("c2", "edit", map[string]any{"path": "inventory.py"}),
		evResult("c2", "edited inventory.py @21", false),
		evCall("c3", "read", map[string]any{"path": "inventory.py"}),
		evResult("c3", "def remove_item(item, stock):\n    del stock[item]", false),
	}
	got := turnToolEvidence(evs, 8)

	if strings.Contains(got, "del stock[itm]") {
		t.Errorf("the stale read's content is still in the block for a member to anchor on:\n%s", got)
	}
	if !strings.Contains(got, "superseded") {
		t.Errorf("the older occurrence must say it was superseded, not silently vanish:\n%s", got)
	}
	if !strings.Contains(got, "del stock[item]") {
		t.Errorf("the newest read's content must survive whole:\n%s", got)
	}
	// The edit between them has a different identity and is untouched.
	if !strings.Contains(got, "edited inventory.py @21") {
		t.Errorf("an unrepeated call lost its result:\n%s", got)
	}
	// The reading rule is stated where the list starts.
	if !strings.Contains(got, "a later result outranks an earlier one") {
		t.Errorf("the block does not state its own ordering rule:\n%s", got)
	}
}

// An error-then-ok history stays legible: the failed first run keeps its status in the stub even
// though its output is gone — "it failed then passed" and "it passed" are different facts.
func TestASupersededFailureKeepsItsStatus(t *testing.T) {
	evs := []event.Event{
		evPromptUser(),
		evCall("c1", "bash", map[string]any{"command": "python3 -m pytest -q"}),
		evResult("c1", "exit 1\nFAILED test_x", true),
		evCall("c2", "bash", map[string]any{"command": "python3 -m pytest -q"}),
		evResult("c2", "exit 0\n10 passed", false),
	}
	got := turnToolEvidence(evs, 8)
	if !strings.Contains(got, "[error, superseded]") {
		t.Errorf("the first run's failure status is gone from the record:\n%s", got)
	}
	if strings.Contains(got, "FAILED test_x") {
		t.Errorf("the superseded failure still carries content:\n%s", got)
	}
	if !strings.Contains(got, "10 passed") {
		t.Errorf("the newest run's output must survive:\n%s", got)
	}
}

// Calls with no identifying arguments are never collapsed into each other — two bare council
// checks are two events, not one asked twice.
func TestUnidentifiedCallsAreNotCollapsed(t *testing.T) {
	evs := []event.Event{
		evPromptUser(),
		evCall("c1", "council", map[string]any{"complete": true}),
		evResult("c1", "does NOT accept: round 1", false),
		evCall("c2", "council", map[string]any{"complete": true}),
		evResult("c2", "does NOT accept: round 2", false),
	}
	got := turnToolEvidence(evs, 8)
	for _, want := range []string{"round 1", "round 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("an unidentified call's result was collapsed away, missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "superseded") {
		t.Errorf("unidentified calls must not supersede each other:\n%s", got)
	}
}
