package app

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// newStoreSession creates a real session (with a session.created event in the store, which
// store appends require) and returns its session.Session value.
func newStoreSession(t *testing.T, a *App, wd string) session.Session {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	s := a.stateLocked(sid).meta
	a.mu.Unlock()
	return s
}

// explorerPrompt is shared by the synchronous (runExplorers) and background
// (dispatchExplorerSteps) fan-out paths, so both must produce the identical prompt.
func TestExplorerPromptParity(t *testing.T) {
	g := planGroup{Agent: "explore", Focus: "the parser", Question: "how are tokens emitted?"}
	// Without a goal: just the investigate line.
	got := explorerPrompt("", g, "")
	if !strings.Contains(got, "Investigate (read-only): the parser") || !strings.Contains(got, "how are tokens emitted?") {
		t.Fatalf("prompt missing focus/question: %q", got)
	}
	if strings.Contains(got, "Overall goal") {
		t.Fatalf("no goal was given but the goal prefix appeared: %q", got)
	}
	// With a goal: the goal prefix leads, the investigate line follows verbatim.
	withGoal := explorerPrompt("review the whole project", g, "")
	if !strings.HasPrefix(withGoal, "Overall goal (context for your investigation): review the whole project") {
		t.Fatalf("goal prefix missing/misplaced: %q", withGoal)
	}
	if !strings.HasSuffix(withGoal, got) {
		t.Fatalf("goal-prefixed prompt must end with the bare prompt; got %q want suffix %q", withGoal, got)
	}
}

// hasWriteStep gates the async-explorer fast path to pure-investigation plans: a plan
// with any delegate/refine step keeps the synchronous executeSteps path.
func TestHasWriteStep(t *testing.T) {
	a := &App{}
	readOnly := []planStep{
		{Title: "a", Strategy: "solo"},
		{Title: "b", Strategy: "parallel", Groups: []planGroup{{Agent: "explore", Focus: "f", Question: "q"}}},
		{Title: "c", Strategy: "scout", Discover: "*.go", Each: "read"},
	}
	if a.hasWriteStep(readOnly) {
		t.Error("pure read-only plan (solo/parallel/scout) must not be flagged as having a write step")
	}
	if !a.hasWriteStep(append(readOnly, planStep{Title: "d", Strategy: "delegate", Agent: "coder", Task: "build"})) {
		t.Error("plan with a delegate step must be flagged as having a write step")
	}
	if !a.hasWriteStep([]planStep{{Title: "r", Strategy: "refine", Task: "improve"}}) {
		t.Error("plan with a refine step must be flagged as having a write step")
	}
}

// asyncExplorersEnabled defaults ON and is turned off by MAGI_ASYNC_EXPLORERS=off (the A/B knob).
func TestAsyncExplorersEnabledEnv(t *testing.T) {
	t.Setenv("MAGI_ASYNC_EXPLORERS", "")
	os.Unsetenv("MAGI_ASYNC_EXPLORERS")
	if !asyncExplorersEnabled() {
		t.Error("async explorers must default ON")
	}
	for _, off := range []string{"off", "0", "false", "no"} {
		t.Setenv("MAGI_ASYNC_EXPLORERS", off)
		if asyncExplorersEnabled() {
			t.Errorf("MAGI_ASYNC_EXPLORERS=%q must disable async explorers", off)
		}
	}
	t.Setenv("MAGI_ASYNC_EXPLORERS", "on")
	if !asyncExplorersEnabled() {
		t.Error("MAGI_ASYNC_EXPLORERS=on must keep async explorers enabled")
	}
}

// dispatchExplorerSteps fans a pure read-only plan's explorer groups out to the BACKGROUND
// (they raise bgOutstanding) instead of blocking, and injects the async note. An all-solo plan
// dispatches nothing and returns false (the caller then takes the solo path).
func TestDispatchExplorerStepsBackgrounds(t *testing.T) {
	a := newOrchApp(t, blockingLLM{}, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"explore": {Name: "explore", System: "x"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		cctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_ = a.Close(cctx)
	})
	s := newStoreSession(t, a, t.TempDir())

	// All-solo plan → nothing to dispatch.
	if a.dispatchExplorerSteps(ctx, s, "goal", []planStep{{Title: "just do it", Strategy: "solo"}}, 0) {
		t.Fatal("an all-solo plan must dispatch no explorers and return false")
	}
	if n := a.bgOutstanding(s.ID); n != 0 {
		t.Fatalf("no explorer should be outstanding after a solo-only plan, got %d", n)
	}

	// Read-only parallel plan → both groups dispatched to the background (blockingLLM keeps
	// them in-flight, so outstanding is observable). dispatch increments outstanding
	// synchronously before returning, so no poll is needed.
	steps := []planStep{{Title: "survey", Strategy: "parallel", Groups: []planGroup{
		{Agent: "explore", Focus: "parser", Question: "qa"},
		{Agent: "explore", Focus: "lexer", Question: "qb"},
	}}}
	a.registerPlanTodos(ctx, s.ID, steps)
	if !a.dispatchExplorerSteps(ctx, s, "goal", steps, 0) {
		t.Fatal("a read-only parallel plan must dispatch explorers and return true")
	}
	if n := a.bgOutstanding(s.ID); n != 2 {
		t.Fatalf("both explorer groups should be dispatched to the background, outstanding=%d want 2", n)
	}
	evs, _ := a.store.Read(ctx, s.ID, 0)
	foundNote := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "read-only explorer") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Error("the async-explorer note must be injected so the orchestrator knows findings will arrive")
	}
}

// routeLLM answers as the orchestrator (recording each request), but BLOCKS any explorer
// subagent call until released — so an explorer stays in-flight for the test.
type routeLLM struct {
	mu       sync.Mutex
	orchReqs []port.ChatRequest
	release  chan struct{}
}

func (l *routeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	// Route by the explorer's task prompt (present only in the CHILD session's messages),
	// not by system text — the explore agent's System leaks into the orchestrator's own
	// system prompt via the subagent roster, so a system-marker check would misroute the
	// orchestrator's call into the blocking explorer branch.
	if requestContains(r, "EXPLORER_TASK_MARKER") {
		ch := make(chan port.ProviderEvent, 2)
		go func() {
			defer close(ch)
			select {
			case <-l.release:
			case <-ctx.Done():
			}
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "explored: found the answer"}
			ch <- port.ProviderEvent{Type: port.ProviderFinish}
		}()
		return ch, nil
	}
	l.mu.Lock()
	l.orchReqs = append(l.orchReqs, r)
	l.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "orchestrator reply"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// When the planner dispatched its read-only explorers to the background, the orchestrator
// parks BEFORE running the model (nothing to synthesize yet) but stays responsive: a user
// interjection during the exploration wakes it and is answered inline, rather than being
// ignored until the ~85s fan-out completes (the live s_b5b014e failure). Proven two ways:
// (1) no model call happens before findings/interjection arrive (the early park held), and
// (2) the orchestrator's FIRST model call already carries the interjection.
func TestAsyncExplorerParkAnswersInterjection(t *testing.T) {
	rl := &routeLLM{release: make(chan struct{})}
	a := newOrchApp(t, rl, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{
			"explore": {Name: "explore", System: "EXPLORER_SYS_MARKER", Tools: []string{"read"}},
		},
	})
	orch := AgentSpec{Name: "default", System: "ORCH_SYS_MARKER"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		cctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
		defer cc()
		_ = a.Close(cctx)
	})
	s := newStoreSession(t, a, t.TempDir())

	// Seed the turn with the user's task.
	if err := a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorUser, ID: "u"}, "review the project"); err != nil {
		t.Fatal(err)
	}
	// The planner's two effects on a pure read-only plan: dispatch the explorer to the
	// background, and inject the async note (which makes the note — not the user prompt —
	// the last event, so lastIsUserSteer is false and the early park engages).
	a.dispatch(ctx, s, 0, port.SpawnRequest{Agent: "explore", Prompt: "EXPLORER_TASK_MARKER Investigate the codebase"})
	a.setAwaitExplorers(s.ID, true)
	a.injectAsyncExplorerNote(ctx, s.ID, 1)

	done := make(chan struct{})
	go func() { defer close(done); _, _ = a.runLoop(ctx, s, orch, 0, 8, true) }()

	// The loop seeds (step 0) and reaches the early park. It must NOT have run the
	// orchestrator model — there are no findings to synthesize yet.
	time.Sleep(150 * time.Millisecond)
	rl.mu.Lock()
	n := len(rl.orchReqs)
	rl.mu.Unlock()
	if n != 0 {
		t.Fatalf("orchestrator ran the model before any findings arrived (%d reqs) — the early park failed", n)
	}

	// User interjects mid-exploration. It must wake the parked orchestrator and be
	// answered inline while the explorer is still blocked.
	if err := a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorUser, ID: "u"}, "INTERJECT_MARKER what time is it"); err != nil {
		t.Fatal(err)
	}
	a.bgWake(s.ID)

	deadline := time.Now().Add(3 * time.Second)
	for {
		rl.mu.Lock()
		answered := false
		var first port.ChatRequest
		hasFirst := len(rl.orchReqs) > 0
		if hasFirst {
			first = rl.orchReqs[0]
		}
		for _, r := range rl.orchReqs {
			if requestContains(r, "INTERJECT_MARKER") {
				answered = true
				break
			}
		}
		rl.mu.Unlock()
		if answered {
			// The orchestrator's very first model call already carries the interjection →
			// it parked until the interjection arrived instead of running a premature,
			// findings-less review.
			if !requestContains(first, "INTERJECT_MARKER") {
				t.Fatal("orchestrator's first model call lacked the interjection — it ran before parking")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("interjection during exploration was never answered — the orchestrator stayed blocked (the s_b5b014e bug)")
		}
		if a.bgOutstanding(s.ID) == 0 {
			t.Fatal("explorer completed before the interjection was answered — test lost its window")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Release the explorer; the loop synthesizes from its result and finishes.
	close(rl.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not finish after the explorer completed")
	}
}
