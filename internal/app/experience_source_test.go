package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// sourceRecorder keeps what was proposed and answers nothing. (distil_test has its own recorder
// that counts rather than keeps; this one needs the contribution itself.)
type sourceRecorder struct{ got []port.Contribution }

func (r *sourceRecorder) Retrieve(context.Context, string, []string) ([]port.Memory, []port.Skill, error) {
	return nil, nil, nil
}

func (r *sourceRecorder) Propose(_ context.Context, c port.Contribution) error {
	r.got = append(r.got, c)
	return nil
}

// What an agent learned carries what it was doing when it learned it.
//
// The store writes Source as a line at the end of the body and the console draws it, and every
// entry said the same word: "agent". That answers who wrote it, which nobody is asking — the
// question in front of a rule you did not write is what it came out of.
func TestWhatWasLearnedCarriesTheTaskItWasLearnedDuring(t *testing.T) {
	exp := &sourceRecorder{}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("remember", `{"text":"the staging database is restored on Mondays","scope":"project"}`),
		textStep("noted"),
	}}
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	reg.Register(builtin.Remember{})
	a := closeAfter(t, New(store, llm, reg, bus.New(), nil,
		Config{Permission: "allow", Experience: exp}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})

	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	const task = "find out why the invoice job double-charges on retry"
	a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: task}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
	})
	waitIdle(t, a, sid)

	if len(exp.got) != 1 {
		t.Fatalf("%d contributions reached the store", len(exp.got))
	}
	src := exp.got[0].Source
	if !strings.Contains(src, "double-charges on retry") {
		t.Errorf("the source does not say what was being done: %q", src)
	}
	if !strings.HasPrefix(src, "agent") {
		t.Errorf("the source lost who wrote it: %q", src)
	}
}
