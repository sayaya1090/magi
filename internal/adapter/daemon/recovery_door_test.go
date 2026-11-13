package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
)

// recoveryEngine answers the recovery doors and remembers how it was asked.
type recoveryEngine struct {
	*fakeEngine
	got   []string
	state app.RecoveryState
	fail  error
}

func (r *recoveryEngine) RecoveryStateOf(_ context.Context, sid string) app.RecoveryState {
	r.got = append(r.got, "state "+sid)
	return r.state
}

func (r *recoveryEngine) ResolveCall(_ context.Context, sid, callID, verdict, note string) error {
	r.got = append(r.got, fmt.Sprintf("resolve %s %s %s %s", sid, callID, verdict, note))
	return r.fail
}

func (r *recoveryEngine) RetryRecovery(_ context.Context, sid string) error {
	r.got = append(r.got, "retry "+sid)
	return r.fail
}

func ask(t *testing.T, eng Engine, req Request) Response {
	t.Helper()
	d, ok := answers[req.Method]
	if !ok {
		t.Fatalf("%s is not a door", req.Method)
	}
	return d.run(context.Background(), eng, req)
}

// A screen asks what the restart left, says what happened to a call, or asks to look again; each
// answer carries the state as it now stands, so the banner never draws a guess.
func TestRecoveryDoorsMapBothDirections(t *testing.T) {
	st := app.RecoveryState{Kind: app.RecoveryNeedsCheck, MessageID: "m1",
		Uncertain: []app.UncertainCall{{CallID: "c1", Tool: "write", Target: "a.txt"}}}
	e := &recoveryEngine{fakeEngine: &fakeEngine{}, state: st}

	r := ask(t, e, Request{Method: "recovery-state", Session: "s1"})
	if !r.OK || r.Recovery == nil || r.Recovery.Kind != app.RecoveryNeedsCheck ||
		len(r.Recovery.Uncertain) != 1 || r.Recovery.Uncertain[0].Target != "a.txt" {
		t.Fatalf("recovery-state: %+v", r)
	}

	r = ask(t, e, Request{Method: "resolve-call", Session: "s1", CallID: "c1", Name: "not_ran", Text: "file absent"})
	if !r.OK || r.Recovery == nil {
		t.Fatalf("resolve-call must answer with the state after it: %+v", r)
	}

	r = ask(t, e, Request{Method: "recovery-retry", Session: "s1"})
	if !r.OK || r.Recovery == nil {
		t.Fatalf("recovery-retry must answer with the state after it: %+v", r)
	}

	want := []string{"state s1", "resolve s1 c1 not_ran file absent", "state s1", "retry s1", "state s1"}
	if fmt.Sprint(e.got) != fmt.Sprint(want) {
		t.Fatalf("asked %q, want %q", e.got, want)
	}
}

func TestRecoveryDoorsRefuseWhatTheyCannotDo(t *testing.T) {
	e := &recoveryEngine{fakeEngine: &fakeEngine{}}
	for _, req := range []Request{
		{Method: "recovery-state"},
		{Method: "resolve-call", Session: "s1", Name: "ran"},
		{Method: "resolve-call", Session: "s1", CallID: "c1"},
		{Method: "recovery-retry"},
	} {
		if r := ask(t, e, req); r.OK || r.Err == "" {
			t.Errorf("%+v must be refused in words: %+v", req, r)
		}
	}
	if len(e.got) != 0 {
		t.Fatalf("a refused request reached the engine: %q", e.got)
	}
	e.fail = errors.New("no such call")
	if r := ask(t, e, Request{Method: "resolve-call", Session: "s1", CallID: "zz", Name: "ran"}); r.OK || r.Err != "no such call" {
		t.Fatalf("the engine's refusal must reach the screen: %+v", r)
	}
	if r := ask(t, e, Request{Method: "recovery-retry", Session: "s1"}); r.OK || r.Err != "no such call" {
		t.Fatalf("a retry that failed again must say so: %+v", r)
	}
}
