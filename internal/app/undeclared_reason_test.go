package app

import (
	"strings"
	"testing"
)

// The banner a turn lands under when the agent never declared it finished must not deny councils
// the run's own log records.
//
// It used to say "so no council read it", flatly. On fix-git the agent stopped mid-merge and asked
// the council whether waiting for the user's wording choice was right; three members read the work
// and answered done, unanimously, at 0.85-0.9 confidence. The turn then landed announcing that no
// council had read it. Both statements were in the same event file.
//
// What is always true is narrower: no council was asked to ACCEPT the work. Only a completion
// declaration asks that, and a question council answers "this is their reading, not a decision".
func TestUndeclaredReasonDoesNotDenyCouncilsThatRead(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readings int
		wants    []string
	}{
		{"none", 0, []string{"never declared the task finished", "no council was asked to accept the work"}},
		{"one", 1, []string{"one council did read it", "judges the question, not whether the work is done"}},
		{"several", 3, []string{"3 councils did read it"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := undeclaredReason(tc.readings, 0)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("reason should contain %q, got: %s", want, got)
				}
			}
			// The retired sentence, in any of its readings.
			if strings.Contains(got, "no council read it") {
				t.Errorf("the banner still claims no council read the work: %s", got)
			}
			if tc.readings > 0 && strings.Contains(got, "no council") &&
				!strings.Contains(got, "no council was asked to accept") {
				t.Errorf("with %d readings the banner must not say no council, got: %s", tc.readings, got)
			}
			if !strings.HasSuffix(got, "the work stands as it was left") {
				t.Errorf("the banner must still say what happened to the work, got: %s", got)
			}
		})
	}
}
