package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// spyCouncil records the request it was convened with, and whether it was convened at all.
type spyCouncil struct {
	got    port.DeliberationRequest
	called bool
}

func (c *spyCouncil) Deliberate(_ context.Context, r port.DeliberationRequest) (council.Deliberation, error) {
	c.got, c.called = r, true
	return council.Deliberation{Decision: council.Done}, nil
}
func (c *spyCouncil) Advise(context.Context, port.AdviceRequest) (string, error) { return "yes", nil }

// A council with nothing to read does not sit.
//
// Every part of what it judges comes from one store read: the task, the tool evidence for what
// the turn ran, the guidance the agent bound itself to, the world-diff's anchor, the objections
// already made, the turn's last words. Read answers (nil, err) when the log cannot be read, and
// that error was discarded — so all of them went empty and the panel convened anyway.
//
// Measured before the fix: councilAdvice returned "The council accepts that the task is finished"
// on a request whose Task was the empty string. A verdict on a completion claim, out of a fact
// about the disk.
//
// The healthy half is asserted first and is not decoration: without it, "never convene at all"
// passes this test, and that is a worse bug than the one being fixed.
func TestACouncilWithNothingToReadDoesNotSit(t *testing.T) {
	ctx := context.Background()
	spy := &spyCouncil{}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("ok")}}
	a, wd := newApp(t, llm, Config{Permission: "allow", Council: spy})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "build the parser"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	s := a.sessionInfo(ctx, sid)

	out, err := a.councilAdvice(ctx, s, nil, 0, "", true)
	if err != nil || !spy.called {
		t.Fatalf("a readable log must convene the council: called=%v err=%v", spy.called, err)
	}
	if spy.got.Task != "build the parser" {
		t.Fatalf("the council was convened without the task it judges against: %q", spy.got.Task)
	}
	if out == "" {
		t.Fatal("a convened council said nothing")
	}

	spy.called = false
	boom := errors.New("the log is on a disk that went away")
	a.store = unreadableStore{Store: a.store, err: boom}

	blind, berr := a.councilAdvice(ctx, s, nil, 0, "", true)
	if spy.called {
		t.Errorf("the council was convened on a log nobody could read, with Task=%q", spy.got.Task)
	}
	if berr == nil {
		t.Fatalf("an unreadable log produced a verdict instead of a refusal: %q", blind)
	}
	if !errors.Is(berr, boom) || !strings.Contains(berr.Error(), "could not be read") {
		t.Errorf("the refusal does not say what went wrong: %v", berr)
	}
	if blind != "" {
		t.Errorf("a refusal must not also carry advice: %q", blind)
	}
}
