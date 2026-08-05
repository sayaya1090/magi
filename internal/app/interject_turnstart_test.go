package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// startTriageLLM answers the turn-start re-evaluation structurally (per the waiting message's
// text) and runs a positional script for ordinary turns, recording every ordinary request so a
// test can assert what the model was actually shown.
type startTriageLLM struct {
	mu       sync.Mutex
	steps    [][]port.ProviderEvent
	call     int
	decide   func(waiting string) string // "answer" | "append" | "queue"
	seenSys  []string
	seenMsgs []string
}

func (f *startTriageLLM) StreamChat(_ context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	send := func(evs []port.ProviderEvent) (<-chan port.ProviderEvent, error) {
		ch := make(chan port.ProviderEvent, 8)
		for _, e := range evs {
			ch <- e
		}
		close(ch)
		return ch, nil
	}
	if strings.Contains(r.System, "has been waiting since before the task you are about to start") {
		// A prior mini-step already ran the route tool: end the mini-turn.
		if n := len(r.Messages); n > 0 {
			for _, p := range r.Messages[n-1].Parts {
				if p.Kind == session.PartToolResult {
					return send(nil)
				}
			}
		}
		waiting := ""
		if len(r.Messages) > 0 {
			waiting = partsText(r.Messages[0].Parts)
		}
		switch f.decide(waiting) {
		case "append":
			return send([]port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_a", Name: "route_interjection",
					Args: json.RawMessage(`{"action":"append","reason":"part of the same job"}`)}},
				{Type: port.ProviderFinish}})
		case "queue":
			return send([]port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_q", Name: "route_interjection",
					Args: json.RawMessage(`{"action":"queue","reason":"separate work"}`)}},
				{Type: port.ProviderFinish}})
		default:
			return send(textStep("answered from what I already know"))
		}
	}
	f.mu.Lock()
	f.seenSys = append(f.seenSys, r.System)
	var b strings.Builder
	for _, m := range r.Messages {
		b.WriteString(partsText(m.Parts) + "\n")
	}
	f.seenMsgs = append(f.seenMsgs, b.String())
	evs := textStep("done")
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	}
	f.call++
	f.mu.Unlock()
	return send(evs)
}

// startWithWaiting seeds a session that already has one message waiting in the queue, then runs a
// turn — the shape the run goroutine reaches when a drain left something behind.
func startWithWaiting(t *testing.T, decide func(string) string, waiting string) (*App, session.SessionID, *startTriageLLM) {
	t.Helper()
	a, sid, llm, _ := startWithWaitingCouncil(t, decide, waiting, true)
	return a, sid, llm
}

// startWithWaitingCouncil is startWithWaiting with the council stubbed, so a test can read the Task
// the members were actually handed — the completion contract the turn is judged against.
func startWithWaitingCouncil(t *testing.T, decide func(string) string, waiting string, leftover bool) (*App, session.SessionID, *startTriageLLM, *fakeCouncil) {
	t.Helper()
	llm := &startTriageLLM{decide: decide, steps: [][]port.ProviderEvent{textStep("the turn's own answer")}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// route_interjection is registered by RegisterOrchestration, not Default — without it the
	// mini-turn cannot route and every disposition collapses to "keep waiting".
	reg := builtin.Default()
	builtin.RegisterOrchestration(reg, false)
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	// The waiting message: a real prompt event, queued, exactly as a mid-turn steer leaves it.
	if err := a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "tui"}, waiting); err != nil {
		t.Fatal(err)
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	var msgID string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(partsText(d.Parts), waiting) {
				msgID = d.MessageID
			}
		}
	}
	a.enqueueInterject(ctx, sid, msgID, waiting)
	if leftover {
		// A finish boundary has already been past it and it is still waiting. Only those are
		// re-triaged; an entry the current turn's own boundary has not reached yet stays with it.
		a.markBoundarySeen(sid)
	}

	submitSync(t, a, sid, "TASK write the parser")
	return a, sid, llm, fc
}

// Answerable now. The previous turn's result is in the transcript, which is half the reason to look
// again — a message that could not be answered while the work was in flight often can be once it
// lands. It leaves the queue and never runs as a turn of its own.
func TestAWaitingMessageAnswerableNowIsAnsweredAtTurnStart(t *testing.T) {
	a, sid, _ := startWithWaiting(t, func(string) string { return "answer" }, "QQQ what does this repo do")

	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("an answered message must leave the queue, %d left", left)
	}
	if !answeredInReplyTo(t, a, sid, "QQQ") {
		t.Error("the answer carries no InReplyTo naming the question — the two will not pair on screen")
	}
	if res := resurfacedPrompts(t, a, sid); len(res) != 0 {
		t.Errorf("it was answered, so nothing should re-surface: %q", res)
	}
}

// Belongs with the task starting now. It is folded in — the steer constraint reaches the model —
// and the turn's finish says so, which is what moves its bubble up beside the answer.
func TestAWaitingMessageThatBelongsWithTheTurnIsFoldedIn(t *testing.T) {
	a, sid, _ := startWithWaiting(t, func(string) string { return "append" }, "WWW also handle the escape sequences")

	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("a folded message must leave the queue, %d left", left)
	}
	// injectSteerConstraint is how the fold reaches the running turn.
	if !promptContains(t, a, sid, "Mid-task steer", "WWW also handle the escape sequences") {
		t.Error("the folded message never reached the turn as a steer constraint")
	}
	// And the turn is recorded as having answered it, so the bubble pairs with this turn's reply.
	if !answeredSignalFor(t, a, sid, "WWW") {
		t.Error("no interjection.answered for the folded message — its bubble stays looking unanswered")
	}
	if res := resurfacedPrompts(t, a, sid); len(res) != 0 {
		t.Errorf("it was folded into this turn, so nothing should re-surface: %q", res)
	}
	// NOT asserted here, deliberately: reviewWaitingAtTurnStart also folds the text into the
	// in-memory turnTask, and nothing observable in this test depends on it. turnTask reaches the
	// stuck nudge and the late-interjection label, not the council — councilAdvice builds its task
	// from lastUserPromptText, and the steer constraint is written by ActorSystem. The assignment
	// is there because the mid-turn route path (loop.go, same helper) does the same thing, and the
	// two disagreeing about what the turn is working on would be its own defect.
}

// Anything still queued when a turn starts becomes VISIBLE to the model, whether or not it is a
// leftover worth re-triaging. This is the part that was missing outright: a queued message is
// masked out of the context (liveEvents drops deferred prompts) and nothing else names it, so for
// the whole turn it did not exist and the model could not route it even if it wanted to.
//
// `leftover: false` on purpose — the note costs no model call, so it is not rationed the way the
// re-triage is, and this is the ordinary case rather than the rare one.
func TestAWaitingMessageIsShownToTheModelEvenWhenNotRetriaged(t *testing.T) {
	a, sid, llm, _ := startWithWaitingCouncil(t, func(string) string { return "queue" }, "ZZZ later, rewrite the docs", false)

	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 1 {
		t.Fatalf("separate work must keep waiting, queue holds %d", left)
	}
	llm.mu.Lock()
	seen := strings.Join(llm.seenMsgs, "\n")
	llm.mu.Unlock()
	if !strings.Contains(seen, "ZZZ later, rewrite the docs") {
		t.Errorf("the waiting message never reached the model — it is masked from context and had "+
			"no note, so the turn cannot route it:\n%s", seen)
	}
}

// promptContains reports whether some prompt fact carries both markers.
func promptContains(t *testing.T, a *App, sid session.SessionID, markers ...string) bool {
	t.Helper()
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		txt := partsText(d.Parts)
		all := true
		for _, m := range markers {
			if !strings.Contains(txt, m) {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

// answeredSignalFor reports whether interjection.answered was recorded for the message carrying
// marker — the signal the TUI reads to pair that bubble with the turn's reply.
func answeredSignalFor(t *testing.T, a *App, sid session.SessionID, marker string) bool {
	t.Helper()
	ids := msgIDsCarrying(t, a, sid, marker)
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypeInterjectionAnswered {
			continue
		}
		var d event.InterjectionAnsweredData
		if json.Unmarshal(e.Data, &d) == nil && ids[d.MessageID] {
			return true
		}
	}
	return false
}
