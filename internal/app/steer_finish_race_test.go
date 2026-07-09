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

// blockingCouncil blocks on its FIRST Deliberate call (signalling started, waiting on
// release) so a test can land a user steer at exactly the moment the council is rendering
// a verdict, then returns scripted deliberations (the last repeats).
type blockingCouncil struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	once    sync.Once
	delibs  []council.Deliberation
	calls   int
}

func (c *blockingCouncil) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	c.mu.Lock()
	i := c.calls
	c.calls++
	c.mu.Unlock()
	if i == 0 {
		c.once.Do(func() { close(c.started) })
		select {
		case <-c.release:
		case <-ctx.Done():
			return council.Deliberation{}, ctx.Err()
		}
	}
	if i < len(c.delibs) {
		return c.delibs[i], nil
	}
	return c.delibs[len(c.delibs)-1], nil
}

func (c *blockingCouncil) JudgeRevision(ctx context.Context, req port.RevisionJudgeRequest) (port.RevisionVerdict, error) {
	return port.RevisionVerdict{Addressed: true, Reason: "test: addressed"}, nil
}

// blockStreamLLM blocks inside StreamChat on a chosen call index (before emitting any
// event), so a test can land a steer AFTER that step's top-of-loop interjection scan has
// run but BEFORE the assistant's text for the step is persisted — the exact sub-step race.
type blockStreamLLM struct {
	mu       sync.Mutex
	steps    [][]port.ProviderEvent
	call     int
	gateCall int
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (f *blockStreamLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	i := f.call
	f.call++
	var evs []port.ProviderEvent
	if i < len(f.steps) {
		evs = f.steps[i]
	} else {
		evs = textStep("done")
	}
	f.mu.Unlock()
	if i == f.gateCall {
		f.once.Do(func() { close(f.started) })
		select {
		case <-f.release:
		case <-ctx.Done():
			ch := make(chan port.ProviderEvent)
			close(ch)
			return ch, ctx.Err()
		}
	}
	ch := make(chan port.ProviderEvent, 8)
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// triageAwareLLM behaves like recordingLLM for ordinary loop turns (positional script), but
// recognizes the finish-boundary triage mini-turn (queuedTriageSystem in the system prompt)
// and responds STRUCTURALLY per routeAside: if it returns true for the queued message's text,
// the triage turn calls route_interjection → the steer ESCALATES to its own turn; otherwise it
// replies with text → the steer is ANSWERED inline (no new turn). Triage turns do not advance
// the positional script, so escalated steers still line up with their scripted own-turn steps.
type triageAwareLLM struct {
	mu         sync.Mutex
	steps      [][]port.ProviderEvent
	call       int
	routeAside func(aside string) bool // nil ⇒ always answer inline
	// Optional gate: block the FIRST triage call (before it decides) after closing
	// triageStarted, until triageRelease is closed — lets a test land a steer mid-triage.
	triageStarted chan struct{}
	triageRelease chan struct{}
	gateOnce      sync.Once
}

func (f *triageAwareLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if strings.Contains(r.System, "was queued while you were finishing the previous task") {
		if f.triageStarted != nil {
			f.gateOnce.Do(func() {
				close(f.triageStarted)
				<-f.triageRelease
			})
		}
		// A prior mini-step already executed the route tool (its result is the last message):
		// end the turn with no further calls so escalate is decided in exactly two calls.
		if n := len(r.Messages); n > 0 {
			for _, p := range r.Messages[n-1].Parts {
				if p.Kind == session.PartToolResult {
					ch := make(chan port.ProviderEvent)
					close(ch)
					return ch, nil
				}
			}
		}
		aside := ""
		if len(r.Messages) > 0 {
			aside = partsText(r.Messages[0].Parts)
		}
		var evs []port.ProviderEvent
		if f.routeAside != nil && f.routeAside(aside) {
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_route", Name: "route_interjection", Args: json.RawMessage(`{"action":"append","reason":"needs real work"}`)}},
				{Type: port.ProviderFinish},
			}
		} else {
			evs = textStep("triage: answered inline from context")
		}
		ch := make(chan port.ProviderEvent, 4)
		for _, e := range evs {
			ch <- e
		}
		close(ch)
		return ch, nil
	}
	f.mu.Lock()
	evs := textStep("done")
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	}
	f.call++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 8)
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func waitSessionIdle(t *testing.T, a *App, sid session.SessionID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		st, _ := a.stateIf(sid)
		idle := st == nil || (st.cancel == nil && len(st.pendingInterject) == 0)
		a.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session did not go idle within 5s")
}

func steerAnswered(evs []event.Event, marker string) bool {
	for _, e := range evs {
		if e.Type == event.TypePartAppended {
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(d.Part.Text, marker) {
				return true
			}
		}
	}
	return false
}

// A user steer that lands while the agent's FINAL (no-tool) step is streaming — after that
// step's top-of-loop interjection scan, before the assistant text is persisted (loop.go
// appends it only at stream end) — must still run. The turn then finishes (council votes
// done, so no `continue` re-check), and the assistant text is persisted AFTER the steer, so
// the run goroutine's last-message-role safety net (hasUnansweredUserPrompt) is fooled by
// the trailing assistant message: without the finish-boundary re-scan the steer is never
// enqueued and is silently lost — not even queued. Regression for enqueueLateInterjections.
func TestSteerDuringFinalStreamRunsAsOwnTurn(t *testing.T) {
	gl := &blockStreamLLM{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		gateCall: 1, // block the final text step (call index 1)
		steps: [][]port.ProviderEvent{
			toolStep("read", `{"path":"x"}`), // step0: real work (usedTools → council fires)
			textStep("turn 1 done"),          // step1: final text (blocked mid-stream)
			toolStep("read", `{"path":"y"}`), // turn2 step0 (the steer, if it runs)
			textStep("STEER-ANSWERED-3R"),    // turn2 step1
		},
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, gl, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	<-gl.started // final text step is streaming, blocked before emitting text
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "STEER-QUERY-3R please also do this"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	close(gl.release) // stream emits final text (persisted AFTER the steer) → council done → finish

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if !steerAnswered(evs, "STEER-ANSWERED-3R") {
		t.Fatalf("steer that landed during the final-step stream was dropped (never ran as its own turn); finishes=%d", countType(evs, event.TypeTurnFinished))
	}
}

// The finish-boundary re-scan must not DOUBLE-run a steer the safety net already covers.
// When the steer lands after the assistant text is persisted (here: while the council is
// blocked rendering its verdict), it IS the last message, so hasUnansweredUserPrompt
// re-runs it. enqueueLateInterjections must then stay out (last message is user) — exactly
// one extra turn, not two.
func TestSteerAfterAssistantTextRunsExactlyOnce(t *testing.T) {
	bc := &blockingCouncil{
		started: make(chan struct{}),
		release: make(chan struct{}),
		delibs:  []council.Deliberation{{Round: 1, Decision: council.Done}},
	}
	llm := &recordingLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"x"}`),
		textStep("turn 1 done"), // persisted before the council blocks
		toolStep("read", `{"path":"y"}`),
		textStep("STEER-ANSWERED-9Q"),
	}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: bc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	<-bc.started // council blocked AFTER the assistant text was persisted
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "STEER-QUERY-9Q please also do this"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	close(bc.release)

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if !steerAnswered(evs, "STEER-ANSWERED-9Q") {
		t.Fatal("steer landing after the assistant text was dropped")
	}
	if got := countType(evs, event.TypeTurnFinished); got != 2 {
		t.Fatalf("steer must run exactly once: want 2 turn.finished (task + steer), got %d (double-run = the re-scan duplicated the safety net)", got)
	}
}

// A steer during a council CONTINUE round (the round just before the final verdict) is
// caught by the normal top-of-loop scan when the gate loops, so it runs as its own turn.
// Guards that the finish-boundary re-scan didn't disturb the working continue path.
func TestSteerDuringCouncilContinueRoundRuns(t *testing.T) {
	bc := &blockingCouncil{
		started: make(chan struct{}),
		release: make(chan struct{}),
		delibs: []council.Deliberation{
			{Round: 1, Decision: council.Continue, Feedback: "add the missing tests"},
			{Round: 2, Decision: council.Done},
		},
	}
	llm := &recordingLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"x"}`),
		textStep("turn 1 first answer"), // → council round1 (blocks, continue)
		toolStep("read", `{"path":"z"}`),
		textStep("turn 1 revised"), // → council round2 done → finish
		toolStep("read", `{"path":"y"}`),
		textStep("STEER-ANSWERED-7C"),
	}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: bc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	<-bc.started
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "STEER-QUERY-7C please also do this"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	close(bc.release)

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if !steerAnswered(evs, "STEER-ANSWERED-7C") {
		t.Fatal("steer during a council continue round was dropped")
	}
}

// A queued steer the model can ANSWER from context (finish-boundary triage replies with text,
// no route) is resolved inline: the queue drains, no fresh top-level turn runs, and the task
// context is NOT reset. Exactly one turn.finished (the original task) — the steer adds none.
func TestQueuedSteerAnsweredInlineNoNewTurn(t *testing.T) {
	// Turn 1: one real-work step then finish. The queued steer's triage answers inline (routeAside
	// nil ⇒ always answer), so no turn 2 runs — the extra scripted step must stay unused.
	llm := &triageAwareLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"x"}`),
		textStep("task done"),
		textStep("UNEXPECTED-SECOND-TURN"), // must never run
	}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	// Queue the steer before the turn so it is buried mid-transcript (drain triage, not the
	// trailing-message safety net, must handle it).
	a.enqueueInterject(sid, "m_q", "just a quick question — how did it go?")
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if got := countType(evs, event.TypeTurnFinished); got != 1 {
		t.Fatalf("an inline-answered steer must add no turn: want 1 turn.finished, got %d", got)
	}
	if steerAnswered(evs, "UNEXPECTED-SECOND-TURN") {
		t.Fatal("inline-answered steer wrongly escalated to a second top-level turn")
	}
	if n := userPrompts(evs); n != 1 {
		t.Fatalf("inline-answered steer must not re-surface as a user prompt: want 1, got %d", n)
	}
}

// A queued steer the model routes (finish-boundary triage calls route_interjection → real work)
// ESCALATES to its own top-level turn with a fresh contract, and re-surfaces as a user prompt.
func TestQueuedSteerRoutedRunsAsOwnTurn(t *testing.T) {
	llm := &triageAwareLLM{routeAside: func(string) bool { return true }, steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"x"}`),
		textStep("task done"),
		toolStep("read", `{"path":"y"}`), // turn 2 (the escalated steer)
		textStep("STEER-WORK-DONE"),
	}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.enqueueInterject(sid, "m_q", "also refactor the parser")
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if got := countType(evs, event.TypeTurnFinished); got != 2 {
		t.Fatalf("a routed steer must run as its own turn: want 2 turn.finished, got %d", got)
	}
	if !steerAnswered(evs, "STEER-WORK-DONE") {
		t.Fatal("routed steer did not run as its own turn")
	}
	if req := fc.lastReq; !strings.Contains(req.Task, "refactor the parser") {
		t.Fatalf("turn 2 council should judge the escalated steer, got Task=%q", req.Task)
	}
}

// A NEW steer that lands DURING the finish-boundary triage model call (while a queued item is
// being answered inline) must not be dropped. The inline reply is an ActorAgent part persisted
// after the new steer, which buries it for both hasUnansweredUserPrompt (last-message-only) and
// seedPromptIdx (an assistant part marks the prior user prompt answered). The drain's teardown
// must detect the newly-arrived prompt by count and re-surface it. Regression guard.
func TestSteerArrivingDuringTriageNotDropped(t *testing.T) {
	llm := &triageAwareLLM{
		routeAside:    func(string) bool { return false }, // Q1 is answered inline
		triageStarted: make(chan struct{}),
		triageRelease: make(chan struct{}),
		steps: [][]port.ProviderEvent{
			toolStep("read", `{"path":"x"}`),
			textStep("task done"),
			// The concurrent steer's own turn (once re-surfaced):
			toolStep("read", `{"path":"y"}`),
			textStep("STEER-MIDTRIAGE-DONE"),
		},
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.enqueueInterject(sid, "m_q1", "quick question — how did it go?")
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	})

	<-llm.triageStarted // triage for Q1 is mid-call
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "STEER-MIDTRIAGE please do real work"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	close(llm.triageRelease) // triage answers Q1 inline, persisting its reply after the steer

	waitSessionIdle(t, a, sid)
	evs, _ := store.Read(ctx, sid, 0)
	if !steerAnswered(evs, "STEER-MIDTRIAGE-DONE") {
		t.Fatalf("a steer that arrived during triage was silently dropped; finishes=%d", countType(evs, event.TypeTurnFinished))
	}
}

// userPrompts counts genuine user-actor PromptSubmitted events in a log — a
// re-surfaced queued interjection appends a second one, so tests use it to assert
// a steer landed as its own follow-up turn (shared with the interject probes).
func userPrompts(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			n++
		}
	}
	return n
}
