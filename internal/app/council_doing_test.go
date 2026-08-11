package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// A council that is sitting says so where a reader in ANOTHER process can hear it.
//
// The council is the longest single thing a turn does — a median 87 seconds across the recorded
// runs — and it announced itself with two transient events, which reach the terminal drawing this
// daemon and nobody else. From an attached window or the console, a turn in council was a turn
// that had stopped saying anything at all; the same shape as a wedged socket.
//
// So it writes the field that already exists for exactly this: the note a long-running tool leaves
// where an outside reader can ask for it. One mechanism, and both surfaces already read it.
func TestACouncilInSessionSaysSoToAnotherProcess(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Breakdown: council.Breakdown{Done: 3, Voters: 3, Rule: council.RuleMajority},
		Verdicts: []council.Verdict{
			{Member: "Melchior", Decision: council.Done},
			{Member: "Balthasar", Decision: council.Done},
			{Member: "Casper", Decision: council.Done},
		},
	}}}
	// The note is read WHILE the council sits, so the fake reads it from inside Deliberate — after
	// that call returns the turn is over and the line is cleared, which is the point of clearing it.
	var during, afterOne string
	fc.onDeliberate = func(req port.DeliberationRequest) {
		during, _ = fc.app.Doing(fc.sid)
		if req.OnVerdict != nil {
			req.OnVerdict(council.Verdict{Member: "Melchior", Decision: council.Done})
			afterOne, _ = fc.app.Doing(fc.sid)
		}
	}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	fc.app, fc.sid = a, sid

	if _, err := a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, "", true); err != nil {
		t.Fatalf("declaring completion: %v", err)
	}

	if !strings.Contains(during, "council") {
		t.Errorf("while the council sat, an outside reader was told %q", during)
	}
	// It counts as they land: "0 of 3" held for ninety seconds is indistinguishable from a line
	// that got stuck.
	if during == afterOne {
		t.Errorf("the line did not move when a member answered: still %q", afterOne)
	}
	if !strings.Contains(afterOne, "1 of 3") {
		t.Errorf("after one member answered the line reads %q", afterOne)
	}
	// And it is cleared when the council is over, rather than left claiming a wait that ended.
	if got, ok := a.Doing(sid); ok {
		t.Errorf("after the council finished the line still says %q", got)
	}
}
