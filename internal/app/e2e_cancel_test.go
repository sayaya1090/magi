package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// TestE2ECancelOrchestratedThenIdleThenNewRequest runs the FULL scenario against
// a live model: a (forcefully) delegating orchestrator dispatches two subagents
// in parallel, one is cancelled mid-flight, the turn must still return to idle,
// and a new request afterwards must complete. The orchestrator prompt is
// deliberately directive so the weak model reliably delegates.
func TestE2ECancelOrchestratedThenIdleThenNewRequest(t *testing.T) {
	base := os.Getenv("MAGI_E2E_OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	if base == "disabled" || !reachable(base) {
		t.Skipf("ollama not reachable at %s", base)
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	wd := t.TempDir()
	if err := os.WriteFile(wd+"/DESIGN.md", []byte("# Design\n\nA CQRS-lite agent with ports/adapters.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	reg.Register(builtin.Ask{})
	reg.Register(builtin.Report{})
	a := New(store, openai.New(base, os.Getenv("MAGI_E2E_API_KEY")), reg, bus.New(), platform.New(), Config{
		Model:      session.ModelRef{Provider: "openai", Model: model},
		Permission: "allow",
		MaxSteps:   40,
		// Directive prompt: force an immediate parallel delegation so the cancel
		// path is reliably exercised (don't depend on the weak model volunteering).
		System: "You are a DISPATCHER. Your FIRST action MUST be a single call to the task tool with " +
			"tasks:[{agent:\"coder\",prompt:\"review DESIGN.md from a coding view\"},{agent:\"tester\",prompt:\"review " +
			"DESIGN.md from a testing view\"}] — both at once, in parallel. Do NOT read files or write analysis " +
			"yourself before delegating. If a subagent is cancelled or fails, synthesize from whatever results you have " +
			"and finish; never re-dispatch.",
		Agents: map[string]AgentSpec{
			"coder":  {Name: "coder", System: "Review the design from a coding perspective in 3 bullets.", Tools: []string{"read", "grep", "glob", "list", "ask", "report"}},
			"tester": {Name: "tester", System: "Review the design from a testing perspective in 3 bullets.", Tools: []string{"read", "grep", "glob", "list", "ask", "report"}},
		},
	})
	ctx, cancelAll := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancelAll()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: model}})
	if err != nil {
		t.Fatal(err)
	}
	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "Have the coder and tester review DESIGN.md, then synthesize."}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	// Generous: the weak local model can meander for minutes after a cancel; we
	// assert it EVENTUALLY returns to idle (correctness), not that it's fast.
	cancelled, spawns := "", 0
	var trace []string
	finished := false
	deadline := time.After(6 * time.Minute)
	for !finished {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("stream closed before turn finished")
			}
			trace = append(trace, traceLine(e))
			switch e.Type {
			case event.TypeAgentSpawned:
				spawns++
				if cancelled == "" { // cancel the first subagent mid-flight
					var d event.AgentStatusData
					if json.Unmarshal(e.Data, &d) == nil {
						cancelled = d.AgentID
						_ = a.Interrupt(ctx, command.Interrupt{SessionID: session.SessionID(d.AgentID)})
						t.Logf("cancelled subagent %s (%s)", d.AgentID, d.Role)
					}
				}
			case event.TypeTurnFinished:
				finished = true
			case event.TypeError:
				t.Fatalf("turn errored instead of returning to idle: %s", string(e.Data))
			}
		case <-deadline:
			from := 0
			if len(trace) > 40 {
				from = len(trace) - 40
			}
			t.Fatalf("turn did not return to idle after cancel (spawns=%d, cancelled=%q)\nlast events:\n%s",
				spawns, cancelled, strings.Join(trace[from:], "\n"))
		}
	}
	if cancelled == "" {
		t.Fatal("orchestrator never delegated despite the directive prompt — cannot exercise cancel")
	}
	t.Logf("turn reached idle after cancelling %s (spawns=%d)", cancelled, spawns)

	// New request after idle must complete (no subagents needed).
	sub2, cancelSub2, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub2()
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "Reply directly (no subagents) with a one-sentence summary of the design."}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	done2 := false
	deadline2 := time.After(2 * time.Minute)
	for !done2 {
		select {
		case e, ok := <-sub2:
			if !ok {
				t.Fatal("stream closed before the second turn finished")
			}
			if e.Type == event.TypeTurnFinished {
				done2 = true
			}
			if e.Type == event.TypeError {
				t.Fatalf("second request errored: %s", string(e.Data))
			}
		case <-deadline2:
			t.Fatal("second request did not finish after returning to idle")
		}
	}
	t.Log("second request completed after idle")
}

// TestE2ECancelLiveSubagentInterrupts verifies against a LIVE Ollama model that
// interrupting a subagent while its model call is in flight actually cancels it —
// the run returns promptly with a cancellation error rather than hanging until
// the subagent timeout. It spawns the subagent directly (rather than hoping the
// weak orchestrator chooses to delegate) so the cancel path is always exercised.
func TestE2ECancelLiveSubagentInterrupts(t *testing.T) {
	base := os.Getenv("MAGI_E2E_OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	if base == "disabled" || !reachable(base) {
		t.Skipf("ollama not reachable at %s", base)
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}

	wd := t.TempDir()
	if err := os.WriteFile(wd+"/DESIGN.md", []byte("# Design\n\nA CQRS-lite agent with ports/adapters.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Report{})
	a := New(store, openai.New(base, os.Getenv("MAGI_E2E_API_KEY")), reg, bus.New(), platform.New(), Config{
		Model:           session.ModelRef{Provider: "openai", Model: model},
		Permission:      "allow",
		SubagentTimeout: 5 * time.Minute, // long, so an early return proves the interrupt (not the timeout) ended it
		Agents: map[string]AgentSpec{
			"coder": {Name: "coder", System: "Write a very long, detailed, multi-paragraph review of the design.", Tools: []string{"read", "grep", "glob", "list", "report"}},
		},
	})

	ctx, cancelAll := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancelAll()

	parent := session.Session{ID: session.SessionID("s_parent_" + newID()), Workdir: wd, Agent: "default", Model: session.ModelRef{Provider: "openai", Model: model}}

	// Watch the parent for the subagent's spawn so we can interrupt that child.
	sub, cancelSub, err := a.Subscribe(ctx, parent.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	resCh := make(chan port.SpawnResult, 1)
	start := time.Now()
	go func() {
		resCh <- a.spawn(ctx, parent, 0, port.SpawnRequest{
			Agent:  "coder",
			Prompt: "Carefully review DESIGN.md and write an extensive, detailed review — at least 10 paragraphs.",
		})
	}()

	// Interrupt the subagent as soon as it is running.
	childID := ""
	spawnWait := time.After(90 * time.Second)
	for childID == "" {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("parent stream closed before the subagent spawned")
			}
			if e.Type == event.TypeAgentSpawned {
				var d event.AgentStatusData
				if json.Unmarshal(e.Data, &d) == nil {
					childID = d.AgentID
				}
			}
		case <-spawnWait:
			t.Fatal("subagent did not spawn within 90s")
		}
	}
	// Give the model call a moment to actually be in flight, then interrupt.
	time.Sleep(1500 * time.Millisecond)
	if err := a.Interrupt(ctx, command.Interrupt{SessionID: session.SessionID(childID)}); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	t.Logf("interrupted subagent %s mid-run", childID)

	select {
	case res := <-resCh:
		elapsed := time.Since(start)
		t.Logf("subagent returned %v after interrupt (err=%q, %d chars text)", elapsed, res.Err, len(res.Text))
		if elapsed > 60*time.Second {
			t.Errorf("cancel was slow (%v) — interrupt likely didn't reach the live model call", elapsed)
		}
		if res.Err == "" {
			t.Errorf("expected a cancellation error, got a normal result (interrupt had no effect)")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("subagent did not return within 60s of interrupt — cancel hung (would only end at the 5m timeout)")
	}
}
