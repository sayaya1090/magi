package app

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// evidenceApp is an App over a real log, since this reads one.
func evidenceApp(t *testing.T) (*App, session.SessionID) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, sid
}

func convene(t *testing.T, a *App, sid session.SessionID, d event.CouncilConvenedData) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	if err := a.appendFact(context.Background(), sid, event.TypeCouncilConvened, actor, raw); err != nil {
		t.Fatal(err)
	}
}

// A round nobody convened and a round convened with nothing to show are the same struct, so the
// reader has to say which it is separately. A surface that read only the value would draw an empty
// evidence pane for a round that never happened and call it a round with no evidence.
func TestARoundThatNeverConvenedIsNotARoundWithNothingToShow(t *testing.T) {
	a, sid := evidenceApp(t)
	// Some round happened, so the log exists and the answer is about round 1 rather than about an
	// empty file.
	convene(t, a, sid, event.CouncilConvenedData{Round: 7})
	got, found, err := a.CouncilEvidenceOf(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("a round that never convened was found: %+v", got)
	}
	convene(t, a, sid, event.CouncilConvenedData{Round: 1})
	got, found, err = a.CouncilEvidenceOf(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("a round that convened with no evidence reads as one that never convened")
	}
	if got.Round != 1 {
		t.Errorf("the round came back as %d", got.Round)
	}
}

// A round can be convened again after a rebuttal, and what the members saw the SECOND time is what
// the verdicts being read were standing on. Reading the first would show a verdict beside material
// it was not given.
func TestTheLastConveningOfARoundIsWhatTheVerdictsStandOn(t *testing.T) {
	a, sid := evidenceApp(t)
	convene(t, a, sid, event.CouncilConvenedData{Round: 1, Report: "the first account"})
	convene(t, a, sid, event.CouncilConvenedData{Round: 2, Report: "another round entirely"})
	convene(t, a, sid, event.CouncilConvenedData{Round: 1, Report: "the account after the rebuttal"})

	got, found, err := a.CouncilEvidenceOf(context.Background(), sid, 1)
	if err != nil || !found {
		t.Fatalf("round 1: found=%v err=%v", found, err)
	}
	if got.Report != "the account after the rebuttal" {
		t.Errorf("round 1 came back standing on %q", got.Report)
	}
	// And the round in between neither took the answer nor gave one.
	got, found, err = a.CouncilEvidenceOf(context.Background(), sid, 2)
	if err != nil || !found {
		t.Fatalf("round 2: found=%v err=%v", found, err)
	}
	if got.Report != "another round entirely" {
		t.Errorf("round 2 came back standing on %q", got.Report)
	}
}

// Every piece of material the round announced reaches the reader. This is a field-by-field copy
// between two structs, and a field left out of it is silent: the console draws the sections it was
// given and no section is missing from a view that never had it.
func TestEveryPieceOfEvidenceTheRoundAnnouncedReachesTheReader(t *testing.T) {
	a, sid := evidenceApp(t)
	convene(t, a, sid, event.CouncilConvenedData{
		Round: 3, Members: []string{"Melchior", "Balthasar"}, Rule: "unanimous",
		Task: "the request", Plan: "the contract", Report: "the claim",
		Actions: "what the tools produced", Changes: "the diff",
		NoChanges: true, Keep: true, Epoch: 9,
	})
	got, found, err := a.CouncilEvidenceOf(context.Background(), sid, 3)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	want := CouncilEvidence{
		Round: 3, Members: []string{"Melchior", "Balthasar"}, Rule: "unanimous",
		Task: "the request", Plan: "the contract", Report: "the claim",
		Actions: "what the tools produced", Changes: "the diff",
		NoChanges: true,
		// Asked is Keep under the name the reader uses. A rename is where a copy loses a field
		// without anything about it looking wrong.
		Asked: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the round was shown\n%+v\nand the reader was given\n%+v", want, got)
	}
}

// A vote is not the material it was cast on. Every council record in the log carries a `round` — the
// verdicts and the tally as much as the convening — so a reader that matched on the number alone
// would take the LAST of them, and the tally for a round is a record with no evidence in it at all.
// The pane would then be empty for exactly the rounds that finished.
func TestAVoteIsNotTheMaterialItWasCastOn(t *testing.T) {
	a, sid := evidenceApp(t)
	ctx, actor := context.Background(), event.Actor{Kind: event.ActorSystem, ID: "council"}
	convene(t, a, sid, event.CouncilConvenedData{Round: 1, Report: "what the members were shown"})

	// Then the round runs to its end, which writes two more records under the same number.
	vote, _ := json.Marshal(event.CouncilVerdictData{Round: 1, Member: "Melchior", Decision: "done"})
	if err := a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vote); err != nil {
		t.Fatal(err)
	}
	tally, _ := json.Marshal(event.CouncilDecidedData{Round: 1, Decision: "done"})
	if err := a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, tally); err != nil {
		t.Fatal(err)
	}

	got, found, err := a.CouncilEvidenceOf(ctx, sid, 1)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got.Report != "what the members were shown" {
		t.Errorf("after the round finished its evidence reads as %q — the verdict overwrote the "+
			"material it was cast on", got.Report)
	}
}

// A convening this build cannot read is not evidence for round zero. The number is what the reader
// matches on and a record it failed to parse has none — carrying the zero value would answer a
// question about round 0 with a record about some other round entirely.
func TestAConveningThatCannotBeReadIsNotRoundZero(t *testing.T) {
	a, sid := evidenceApp(t)
	ctx := context.Background()
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	// Written by a build whose round is spelled differently: the whole payload fails to parse.
	if err := a.appendFact(ctx, sid, event.TypeCouncilConvened, actor,
		json.RawMessage(`{"round":"one","report":"unreadable here"}`)); err != nil {
		t.Fatal(err)
	}
	got, found, err := a.CouncilEvidenceOf(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("a record that could not be parsed was reported as round 0's evidence: %+v", got)
	}
}

// notShown is what the record carries and the reader deliberately does not, with the reason. The
// list is compared against the two structs below, so a field ADDED to the record is either carried
// to the reader or named here — it cannot arrive and be quietly dropped, which is the failure this
// whole view exists because of (Actions was missing from the record for exactly that long).
var notShown = map[string]string{
	"Epoch": "the guard's mutation count when the round convened — turn-local, and meaningless " +
		"as a display fact; it is recorded so the log can answer 'was there work between these " +
		"two councils', which is not a question this view asks",
}

func TestWhatTheRecordCarriesAndTheReaderDoesNotIsNamed(t *testing.T) {
	fields := func(v any) map[string]bool {
		out := map[string]bool{}
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			out[rt.Field(i).Name] = true
		}
		return out
	}
	record, view := fields(event.CouncilConvenedData{}), fields(CouncilEvidence{})
	// Asked is Keep renamed; the reader has no other name the record does not.
	view["Keep"] = view["Asked"]

	var unaccounted []string
	for name := range record {
		if !view[name] && notShown[name] == "" {
			unaccounted = append(unaccounted, name)
		}
	}
	sort.Strings(unaccounted)
	if len(unaccounted) > 0 {
		t.Errorf("the round announces %v and the reader neither carries them nor says why — a "+
			"surface checking a verdict against its material is missing material and cannot tell",
			unaccounted)
	}
	for name := range notShown {
		if !record[name] {
			t.Errorf("%q is excused from the view and the record no longer has it — the reason is "+
				"about a field that is gone", name)
		}
	}
}
