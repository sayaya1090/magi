package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type nopExperience struct{}

func (nopExperience) Retrieve(context.Context, string, []string) ([]port.Memory, []port.Skill, error) {
	return nil, nil, nil
}
func (nopExperience) Propose(context.Context, port.Contribution) error { return nil }

// The question is asked once, at the finish, and only when asked for.
//
// It costs a model round trip per completed task, and time-to-first-token is most of this tree's
// wall clock — so it is a lever to compare, not a default to assume. Everything about the shape
// here is about not spending that round trip where it cannot pay.
func TestTheFinishAsksWhatWasWorthKeeping(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	a.cfg.Experience = nopExperience{}
	tc := turnCtx{s: parent, agent: AgentSpec{Name: "main"}, depth: 0}

	t.Setenv("MAGI_DISTIL", "")
	if a.askToDistil(context.Background(), tc, &turnState{}) {
		t.Error("it asked with the flag off — the round trip is not free")
	}

	t.Setenv("MAGI_DISTIL", "1")
	ts := &turnState{}
	if !a.askToDistil(context.Background(), tc, ts) {
		t.Fatal("with the flag on it did not ask")
	}
	// Once. The reminder loop beside it is bounded because an unbounded one holds a session open
	// forever; the same reason applies here.
	if a.askToDistil(context.Background(), tc, ts) {
		t.Error("it asked twice in one turn")
	}

	// The question reached the agent, and it names the two shapes the store has — "save something"
	// without them produces a paragraph nobody can retrieve.
	evs, err := a.store.Read(context.Background(), parent.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var asked string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "worth knowing") {
			asked = string(e.Data)
		}
	}
	if asked == "" {
		t.Fatal("no question was put to the agent")
	}
	for what, want := range map[string]string{
		"the memory shape": "MEMORY",
		"the skill shape":  "SKILL",
		"the tool to use":  "remember",
		"leave with none":  "Most turns have nothing",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the question does not mention %s (%q)", what, want)
		}
	}
}

// It does not ask where the answer has nowhere to go, or where the agent could not answer.
func TestItDoesNotAskAQuestionThatCannotBeAnswered(t *testing.T) {
	t.Setenv("MAGI_DISTIL", "1")
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	ctx := context.Background()

	// No store: the answer would have nowhere to land.
	a.cfg.Experience = nil
	if a.askToDistil(ctx, turnCtx{s: parent, agent: AgentSpec{Name: "main"}}, &turnState{}) {
		t.Error("it asked with no experience store configured")
	}

	a.cfg.Experience = nopExperience{}
	// An agent whose allowlist has no `remember` would be asked for something and then refused
	// when it reached for the tool — the same defect the shared-experience pointer once had.
	narrow := AgentSpec{Name: "reader", Tools: []string{"read", "grep"}}
	if a.askToDistil(ctx, turnCtx{s: parent, agent: narrow}, &turnState{}) {
		t.Error("it asked an agent that is not permitted to save anything")
	}
	// A CHILD answers to the tool that spawned it. Asking it directly would put entries in the
	// store that no parent ever saw.
	if a.askToDistil(ctx, turnCtx{s: parent, agent: AgentSpec{Name: "child"}, depth: 1}, &turnState{}) {
		t.Error("it asked a child")
	}
}

// magi does not write the lesson. The question is put to the agent and the agent answers with its
// own tool — a memory magi invented would be retrieved later as though the model had chosen it,
// and nothing afterwards could tell the two apart.
func TestMagiDoesNotWriteTheLessonItself(t *testing.T) {
	t.Setenv("MAGI_DISTIL", "1")
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	rec := &recordingExperience{}
	a.cfg.Experience = rec

	a.askToDistil(context.Background(), turnCtx{s: parent, agent: AgentSpec{Name: "main"}}, &turnState{})
	if rec.proposals != 0 {
		t.Errorf("asking the question wrote %d entries by itself", rec.proposals)
	}
}

type recordingExperience struct{ proposals int }

func (r *recordingExperience) Retrieve(context.Context, string, []string) ([]port.Memory, []port.Skill, error) {
	return nil, nil, nil
}
func (r *recordingExperience) Propose(context.Context, port.Contribution) error {
	r.proposals++
	return nil
}

var _ = session.SessionID("")

// The finish seam actually reaches it.
//
// Every test above calls askToDistil directly, so all of them pass with the call site removed
// from finishTurn — the shape of defect this tree keeps finding, and the reason this one runs a
// whole turn instead.
func TestAnAcceptedTurnIsAskedThroughTheFinishSeam(t *testing.T) {
	t.Setenv("MAGI_DISTIL", "1")
	a, parent, _ := spawnApp(t, &declaringLLM{})
	a.cfg.Experience = nopExperience{}
	ctx := context.Background()

	if err := a.appendPromptText(ctx, parent.ID,
		event.Actor{Kind: event.ActorUser, ID: "u"}, "do the thing"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runLoop(ctx, a.sessionInfo(ctx, parent.ID),
		AgentSpec{Name: "main"}, 0, 8, true); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if !promptContains(t, a, parent.ID, "worth knowing at the START of a future task") {
		t.Error("an accepted turn was never asked what was worth keeping — the finish seam does not reach it")
	}
}

// And an accepted turn with the flag OFF is not asked, through the same path. Without this the
// test above would pass on a seam that asks unconditionally.
func TestAnAcceptedTurnIsNotAskedWhenTheLeverIsOff(t *testing.T) {
	t.Setenv("MAGI_DISTIL", "0")
	a, parent, _ := spawnApp(t, &declaringLLM{})
	a.cfg.Experience = nopExperience{}
	ctx := context.Background()

	if err := a.appendPromptText(ctx, parent.ID,
		event.Actor{Kind: event.ActorUser, ID: "u"}, "do the thing"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runLoop(ctx, a.sessionInfo(ctx, parent.ID),
		AgentSpec{Name: "main"}, 0, 8, true); err != nil {
		t.Fatal(err)
	}
	if promptContains(t, a, parent.ID, "worth knowing at the START of a future task") {
		t.Error("the question was put with the lever off")
	}
}

// declaringLLM does a little work, declares the task finished, then answers.
type declaringLLM struct{ n int }

func (f *declaringLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	switch f.n {
	case 0:
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "t1", Name: "list", Args: []byte(`{"path":"."}`)}}
	case 1:
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c1", Name: "council", Args: []byte(`{"complete":true,"question":""}`)}}
	default:
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "all done"}
	}
	f.n++
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A turn that ends WITHOUT the council accepting is not asked.
//
// The question is "what did we learn from work that was accepted". A turn that ran out of
// reminders and landed undeclared has no verdict behind it, and treating it the same would put
// lessons from unfinished work into the store as though they had been checked.
func TestAnUndeclaredFinishIsNotAsked(t *testing.T) {
	t.Setenv("MAGI_DISTIL", "1")
	// This one uses a tool and then goes quiet — it never declares, so the loop reminds it and
	// eventually lands the work undeclared.
	a, parent, _ := spawnApp(t, &toolThenTextLLM{})
	a.cfg.Experience = nopExperience{}
	ctx := context.Background()

	if err := a.appendPromptText(ctx, parent.ID,
		event.Actor{Kind: event.ActorUser, ID: "u"}, "do the thing"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runLoop(ctx, a.sessionInfo(ctx, parent.ID),
		AgentSpec{Name: "main"}, 0, 12, true); err != nil {
		t.Fatal(err)
	}
	if promptContains(t, a, parent.ID, "worth knowing at the START of a future task") {
		t.Error("a turn that finished undeclared was asked what it learned — nothing checked that work")
	}
}
