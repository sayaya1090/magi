package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// stubCouncil answers with a fixed deliberation and records what it was asked.
type stubCouncil struct {
	out   council.Deliberation
	err   error
	phase string
	task  string
	calls int
}

func (c *stubCouncil) Deliberate(_ context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	c.calls++
	c.phase, c.task = req.Phase, req.Task
	return c.out, c.err
}

// The trigger is unchanged and the council only changes the WORDS. So: with it off, the fixed
// paragraph; with it on and the members saying "not going anywhere", their words; with it on and
// the members saying "on track", the fixed paragraph again — never silence, because the counter
// that fired had already decided the agent should hear something.
func TestTheInterventionCouncilOnlyChangesTheWords(t *testing.T) {
	fixed := "You've made the same call several times"
	for _, tc := range []struct {
		what     string
		on       bool
		out      council.Deliberation
		err      error
		wantSaid string
		wantCall int
	}{
		{"off: the paragraph, and the council is never asked", false,
			council.Deliberation{}, nil, fixed, 0},
		{"on, members say redirect: their words", true,
			council.Deliberation{Decision: council.Continue, Verdicts: []council.Verdict{
				{Member: "Melchior", Lens: "correctness", Feedback: "the proto field is val, the task says value"}}},
			nil, "the proto field is val", 1},
		{"on, members say on track: the paragraph", true,
			council.Deliberation{Decision: council.Done, Verdicts: []council.Verdict{
				{Member: "Melchior", Decision: "done"}}}, nil, fixed, 1},
		{"on, council unreachable: the paragraph", true,
			council.Deliberation{}, context.DeadlineExceeded, fixed, 1},
	} {
		t.Run(tc.what, func(t *testing.T) {
			if tc.on {
				t.Setenv("MAGI_INTERVENTION_COUNCIL", "1")
			} else {
				t.Setenv("MAGI_INTERVENTION_COUNCIL", "")
			}
			stub := &stubCouncil{out: tc.out, err: tc.err}
			a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: stub})
			s := a.sessionInfo(context.Background(), sid)
			g := newRunGuard()
			g.blocked = nudgeThreshold // arm the repeat nudge, exactly as a real run would
			tcx := turnCtx{s: s, agent: AgentSpec{Name: "coder"}, guard: g}

			a.injectStuckNudge(context.Background(), tcx, "make the thing work", nil)

			evs, err := a.store.Read(context.Background(), sid, 0)
			if err != nil {
				t.Fatal(err)
			}
			var last string
			for _, e := range evs {
				if e.Type != event.TypePromptSubmitted || e.Actor.ID != "loop" {
					continue
				}
				var d event.PromptSubmittedData
				if json.Unmarshal(e.Data, &d) == nil {
					for _, p := range d.Parts {
						last = p.Text
					}
				}
			}
			if last == "" {
				t.Fatal("no nudge was emitted, so this asserts nothing")
			}
			if !strings.Contains(last, tc.wantSaid) {
				t.Errorf("the nudge should carry %q:\n%s", tc.wantSaid, last[:min(500, len(last))])
			}
			if stub.calls != tc.wantCall {
				t.Errorf("council asked %d times, want %d", stub.calls, tc.wantCall)
			}
			if tc.wantCall > 0 && stub.phase != port.PhaseIntervention {
				t.Errorf("the council was asked the wrong question: phase %q", stub.phase)
			}
			// The task re-read is the one part that is always true, so it survives either way.
			if !strings.Contains(last, "make the thing work") {
				t.Errorf("the task re-read was dropped:\n%s", last)
			}
		})
	}
}

// It must not be able to stop anything: the advice is a prompt, and the turn control it could have
// touched is untouched.
func TestTheInterventionCouncilEndsNothing(t *testing.T) {
	t.Setenv("MAGI_INTERVENTION_COUNCIL", "1")
	stub := &stubCouncil{out: council.Deliberation{Decision: council.Continue,
		Verdicts: []council.Verdict{{Member: "Casper", Feedback: "stop rebuilding the backups"}}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: stub})
	s := a.sessionInfo(context.Background(), sid)
	g := newRunGuard()
	g.blocked = nudgeThreshold
	a.injectStuckNudge(context.Background(), turnCtx{s: s, agent: AgentSpec{Name: "coder"}, guard: g},
		"do the work", nil)

	if tc := a.takeTurnControl(sid); tc.finish {
		t.Error("a mid-turn reading must never finish the turn")
	}
	// And it is on the record as a council round, so the transcript and the TUI show it.
	evs, _ := a.store.Read(context.Background(), sid, 0)
	var convened, decided int
	for _, e := range evs {
		switch e.Type {
		case event.TypeCouncilConvened:
			convened++
		case event.TypeCouncilDecided:
			decided++
		}
	}
	if convened != 1 || decided != 1 {
		t.Errorf("the round should be visible: convened=%d decided=%d", convened, decided)
	}
}
