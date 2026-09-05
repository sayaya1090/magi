package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// gatingObserver is a TurnObserver that also gates declarations (what the plugin host is).
type gatingObserver struct {
	why   []string
	asked int
	steps int
}

func (g *gatingObserver) UserMessage(string, string)           {}
func (g *gatingObserver) TurnFinished(string, TurnObservation) {}
func (g *gatingObserver) GateDeclaration(ctx context.Context, sid string, steps func(context.Context) ([]port.ChildStep, error)) []string {
	g.asked++
	if st, err := steps(ctx); err == nil {
		g.steps = len(st)
	}
	return g.why
}

// A plugin gate that refuses keeps the declaration from ever reaching the council; one that
// passes changes nothing.
func TestDeclarationGateRunsBeforeTheCouncil(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done, Verdicts: []council.Verdict{
		{Member: "Melchior", Decision: council.Done}}}}}
	var convened int
	fc.onDeliberate = func(port.DeliberationRequest) { convened++ }
	obs := &gatingObserver{why: []string{"3장 만들고 1번 봤다"}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc, Observer: obs,
		CouncilMembers: []council.Member{{Name: "Melchior", Lens: "correctness", Provider: "a"}}})
	got, err := a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, 0, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "refused before any council convenes") || !strings.Contains(got, "3장 만들고 1번 봤다") {
		t.Errorf("the gate's reason must come back verbatim as a refusal, got %q", got)
	}
	if convened != 0 || obs.asked != 1 {
		t.Errorf("a refused declaration must not convene the council (convened=%d asked=%d)", convened, obs.asked)
	}
	obs.why = nil
	if _, err := a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, 0, "", true); err != nil {
		t.Fatal(err)
	}
	if convened != 1 {
		t.Errorf("a passing gate lets the declaration reach the council, convened=%d", convened)
	}
	// A question (not a declaration) never asks the gates.
	obs.asked = 0
	_, _ = a.councilAdvice(context.Background(), a.sessionInfo(context.Background(), sid), nil, 0, "is this right?", false)
	if obs.asked != 0 {
		t.Error("gates are for declarations only")
	}
}
