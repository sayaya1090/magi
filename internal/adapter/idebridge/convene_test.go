package idebridge

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A council folds to one line until it is clicked, and it folds by CONVENE, not by round: every
// council opens at round 1, so two councils in one stream would land on each other by round alone.
// The opening row, its verdicts and its decided row carry the same convene; the decided row is the
// one line that stands for the whole council (closing). Same rule as the TypeScript and Kotlin copies.
func TestCouncilRowsCarryTheirConveneAndMarkTheClosingRow(t *testing.T) {
	var evs []event.Event
	for _, base := range []int64{1, 10} {
		evs = append(evs,
			mk(base, string(event.TypeCouncilConvened), map[string]any{"round": 1, "task": "t"}, nil),
			mk(base+1, string(event.TypeCouncilVerdict), map[string]any{"round": 1, "member": "Melchior", "decision": "done", "rationale": "ok"}, nil),
			mk(base+2, string(event.TypeCouncilDecided), map[string]any{"round": 1, "decision": "done"}, nil),
			mk(base+3, string(event.TypeCouncilDecided), map[string]any{"round": 1, "decision": "continue", "note": "redeclared"}, nil))
	}
	var got []string
	for _, r := range Rows(evs) {
		if r.Who == WhoCouncil {
			got = append(got, fmt.Sprintf("%d/%v/%v", r.Convene, r.Opened, r.Closing))
		}
	}
	want := "1/true/false 1/false/false 1/false/true 0/false/false 2/true/false 2/false/false 2/false/true 0/false/false"
	if strings.Join(got, " ") != want {
		t.Fatalf("council rows (convene/opened/closing):\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}
