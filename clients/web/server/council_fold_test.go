package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/app"
)

// A council is one line in the console, accepted or not, and its votes open when that line is
// clicked (asked 2026-11-15). The page can only fold what it can group: each finished council's
// votes and outcome carry one convene number, the outcome is the closing line, and the votes of a
// council still running carry none, so nothing unfinished is hidden behind a line that is not there.
func TestCouncilRowsCarryTheirConveneAndClosingLine(t *testing.T) {
	rows := []line{{Who: "user", Text: "ask", msg: "m1"}, {Who: "assistant", Text: "answer", msg: "m2"}}
	marks := []app.CouncilMark{
		{After: "m2", Round: 1, Member: "Melchior", Decision: "continue"},
		{After: "m2", Round: 1, Member: "Casper", Decision: "continue"},
		{After: "m2", Round: 1, Decision: "continue", Tally: "0 done, 2 continue of 2"},
		{After: "m2", Round: 1, Member: "Melchior", Decision: "done"},
		{After: "m2", Round: 1, Decision: "done", Tally: "1 done, 0 continue of 1"},
		{After: "m2", Round: 1, Member: "Balthasar", Decision: "done"},
	}
	got := spliceCouncil(rows, marks, []string{"m1", "m2"})
	var council []line
	for _, r := range got {
		if r.Who == "council" {
			council = append(council, r)
		}
	}
	if len(council) != 6 {
		t.Fatalf("council rows = %d, want 6", len(council))
	}
	want := []struct {
		convene int
		closing bool
	}{{1, false}, {1, false}, {1, true}, {2, false}, {2, true}, {0, false}}
	for i, w := range want {
		if council[i].Convene != w.convene || council[i].Closing != w.closing {
			t.Errorf("row %d (%s %q): convene=%d closing=%v, want %d %v", i, council[i].Member, council[i].Decision,
				council[i].Convene, council[i].Closing, w.convene, w.closing)
		}
	}
}
