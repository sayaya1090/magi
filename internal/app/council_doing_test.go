package app

import (
	"context"
	"strings"
	"testing"
	"time"

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
	// Members on DIFFERENT backends, which is the shape where a per-member count means something:
	// they are polled separately, so "1 of 3" is a fact about the round rather than a guess.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc,
		CouncilMembers: []council.Member{
			{Name: "Melchior", Lens: "correctness", Provider: "a"},
			{Name: "Balthasar", Lens: "completeness", Provider: "b"},
			{Name: "Casper", Lens: "risk", Provider: "c"},
		}})
	fc.app, fc.sid = a, sid

	if _, err := a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, 0, "", true); err != nil {
		t.Fatalf("declaring completion: %v", err)
	}

	if !strings.Contains(during, "council") {
		t.Errorf("while the council sat, an outside reader was told %q", during)
	}
	// It counts as they land: "0 of 3" held for ninety seconds is indistinguishable from a line
	// that got stuck. This is the split shape, so the count is what moves.
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

// One call for the whole panel has no partial count to give, and must not pretend to one.
//
// Sharing a backend makes the round a single request: no verdict exists until the whole reply is
// parsed, and all three then arrive together. The line counted verdicts anyway, so it read
// "waiting on the council: 0 of 3 have answered" for the entire median 87 seconds — a sentence that
// is false about the round and, worse, is the exact reading of a council that has died.
func TestAPanelDoesNotClaimNobodyHasAnswered(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Breakdown: council.Breakdown{Done: 3, Voters: 3, Rule: council.RuleMajority},
		Verdicts:  []council.Verdict{{Member: "Melchior", Decision: council.Done}},
	}}}
	var during string
	fc.onDeliberate = func(port.DeliberationRequest) { during, _ = fc.app.Doing(fc.sid) }
	// The default members share a backend, which is the shape almost every run is in.
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	fc.app, fc.sid = a, sid

	if _, err := a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, 0, "", true); err != nil {
		t.Fatalf("declaring completion: %v", err)
	}
	if strings.Contains(during, "0 of") {
		t.Errorf("a single-reply council still claims a count: %q", during)
	}
	if !strings.Contains(during, "one reply") {
		t.Errorf("the line does not say the round is one reply: %q", during)
	}
	if !council.OnePanel(council.DefaultMembers()) {
		t.Error("the default members no longer share a backend, so this test is measuring the other shape")
	}
}

// The line carries how long it has been waiting, because that is the part that moves.
//
// A stored note is read by polling. Written once at the start it stops changing at exactly the
// moment somebody starts wondering whether it has, and a reader cannot tell a council that is
// thinking from one whose socket died.
func TestTheCouncilLineCarriesHowLongItHasWaited(t *testing.T) {
	one := councilDoing(3, 0, true, 72*time.Second)
	if !strings.Contains(one, "1m12s") {
		t.Errorf("the panel line does not say how long: %q", one)
	}
	split := councilDoing(3, 1, false, 40*time.Second)
	if !strings.Contains(split, "1 of 3") || !strings.Contains(split, "40s") {
		t.Errorf("the split line lost its count or its clock: %q", split)
	}
	// At zero there is nothing to say yet, and " — 0s so far" is noise on the first draw.
	if strings.Contains(councilDoing(3, 0, true, 0), "so far") {
		t.Errorf("the first line pads itself with a zero: %q", councilDoing(3, 0, true, 0))
	}
}
