package app

import (
	"context"
	"encoding/json"
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

// The idle-park aside handler (handleAside) is the tool-capable mini-loop that replaced the
// tool-free answerAside. These probes pin its two jobs — reply to chitchat, or SIGNAL a steer
// (route_interjection / cancel_dispatch, optionally after ask_user) — and its queue disposition
// (a routed steer is left queued for the loop's turnControl drain; a resolved chitchat reply or
// bare cancel is consumed here so it does not also re-run as its own turn).

// scriptLLM returns a scripted sequence of responses, one per StreamChat call — same shape as
// fakeLLM but standalone so these probes don't depend on loop_test ordering.
type scriptLLM struct {
	steps [][]port.ProviderEvent
	call  int
}

func (f *scriptLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 16)
	var evs []port.ProviderEvent
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	} else {
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}}
	}
	f.call++
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func asideText(s string) []port.ProviderEvent {
	return []port.ProviderEvent{{Type: port.ProviderText, Text: s}, {Type: port.ProviderFinish}}
}

func asideCall(name, args string) []port.ProviderEvent {
	return []port.ProviderEvent{
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_" + name, Name: name, Args: json.RawMessage(args)}},
		{Type: port.ProviderFinish},
	}
}

// newAsideApp builds an App with the given scripted provider and an interactive top-level
// session, returning the session snapshot the handler operates on.
func newAsideApp(t *testing.T, llm port.LLMProvider) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	// The idle-park handler offers exactly these three signal/interaction tools; production
	// registers them in cmd/magi, so mirror that here (they are not in builtin.Default()).
	reg.Register(builtin.RouteInterjection{})
	reg.Register(builtin.CancelDispatch{})
	reg.Register(builtin.AskUser{})
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: true})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.sessionInfo(context.Background(), sid)
}

// (a) A chitchat aside: the handler replies (persisted) and does NOT act — so the caller
// re-parks — and it consumes the queued item so it will not also re-run as its own turn.
func TestAsideChitchatRepliesAndConsumes(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideText("아직 진행 중입니다.")}}
	a, s := newAsideApp(t, llm)
	a.enqueueInterject(context.Background(), s.ID, "m1", "how's it going?")

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "big delegated task", "m_a1", "how's it going?")
	if acted {
		t.Fatalf("chitchat must not act on the work (acted=true)")
	}
	if a.hasPendingInterject(s.ID) {
		t.Errorf("chitchat reply must consume the queued interjection (still pending)")
	}
	// The reply was persisted as an assistant text part.
	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	if !anyPartText(evs, "아직 진행 중입니다.") {
		t.Errorf("chitchat reply was not persisted")
	}
}

// (b) A redirect steer: route_interjection fires (enqueue-first satisfied), the handler acts,
// turnControl records the redirect, and the item is LEFT queued for the loop's drain to apply.
func TestAsideRedirectRoutesAndLeavesQueued(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{
		asideCall("route_interjection", `{"action":"redirect","reason":"only docs now"}`),
		asideText("전환하겠습니다."),
	}}
	a, s := newAsideApp(t, llm)
	a.enqueueInterject(context.Background(), s.ID, "m1", "actually only look under docs/")

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "read the whole repo", "m_a2", "actually only look under docs/")
	if !acted {
		t.Fatalf("a redirect steer must act (break the park)")
	}
	if tc := a.takeTurnControl(s.ID); tc.route != "redirect" {
		t.Errorf("turnControl.route = %q, want redirect", tc.route)
	}
	if !a.hasPendingInterject(s.ID) {
		t.Errorf("a routed redirect must stay queued for the turnControl drain to apply")
	}
}

// (c) An append steer behaves the same way (acts + leaves queued) but records route=append.
func TestAsideAppendRoutes(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{
		asideCall("route_interjection", `{"action":"append","reason":"then also write a README"}`),
	}}
	a, s := newAsideApp(t, llm)
	a.enqueueInterject(context.Background(), s.ID, "m1", "after that, also write a README")

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "build the parser", "m_a3", "after that, also write a README")
	if !acted {
		t.Fatalf("an append steer must act")
	}
	if tc := a.takeTurnControl(s.ID); tc.route != "append" {
		t.Errorf("turnControl.route = %q, want append", tc.route)
	}
}

// (d) ask_user then route: the mini-loop must run more than one step — clarify via ask_user,
// receive the human's pick, then route on the next step. Drives the interactive answer path.
func TestAsideAskThenRoute(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{
		asideCall("ask_user", `{"questions":[{"question":"redirect or just append?","options":["redirect","append"]}]}`),
		asideCall("route_interjection", `{"action":"append","reason":"user chose append"}`),
	}}
	a, s := newAsideApp(t, llm)
	a.enqueueInterject(context.Background(), s.ID, "m1", "change it, but I'm not sure how")

	// Answer the ask_user question as soon as it is published.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub, cancelSub, err := a.Subscribe(ctx, s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		for e := range sub {
			if e.Type == event.TypeQuestionRequested {
				var d event.QuestionRequestedData
				if json.Unmarshal(e.Data, &d) == nil {
					_ = a.RespondQuestion(ctx, command.RespondQuestion{SessionID: s.ID, CallID: d.CallID, Answer: "append"})
					return
				}
			}
		}
	}()

	acted := a.handleAside(ctx, AgentSpec{Name: "default"}, s, 0, "refactor the loop", "m_a4", "change it, but I'm not sure how")
	if !acted {
		t.Fatalf("ask_user→route must ultimately act")
	}
	if tc := a.takeTurnControl(s.ID); tc.route != "append" {
		t.Errorf("turnControl.route = %q, want append (after ask_user)", tc.route)
	}
	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		t.Errorf("ask_user question was never asked (single-step loop?)")
	}
}

// (e) The enqueue-first invariant: route_interjection requires a pending interjection. Without
// one it must error (and the handler does not act), guarding against a regression where the
// handler forgets to enqueue before signalling.
func TestAsideRouteWithoutQueueErrors(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{
		asideCall("route_interjection", `{"action":"redirect","reason":"switch"}`),
		asideText("ok"),
	}}
	a, s := newAsideApp(t, llm)
	// NOTE: intentionally NOT enqueuing — simulate the invariant being violated.

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "some task", "m_a5", "switch to X")
	if acted {
		t.Errorf("route without a queued interjection must not act")
	}
	if tc := a.takeTurnControl(s.ID); tc.route != "" {
		t.Errorf("turnControl.route = %q, want empty (route errored)", tc.route)
	}
}

// anyPartText reports whether any part.appended event carries the given text.
func anyPartText(evs []event.Event, want string) bool {
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartText && strings.Contains(d.Part.Text, want) {
			return true
		}
	}
	return false
}
