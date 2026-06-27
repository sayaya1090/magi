package app

import (
	"context"
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
)

// TestE2ESteerDuringSubagent verifies the sidecar model against a real model:
// while a background subagent runs, a steered user message gets a response from
// the main orchestrator BEFORE the subagent finishes (i.e. main stays
// responsive, doesn't block). Run with a live Ollama.
func TestE2ESteerDuringSubagent(t *testing.T) {
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

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	llm := openai.New(base, os.Getenv("MAGI_E2E_API_KEY"))
	a := New(store, llm, reg, bus.New(), platform.New(), Config{
		Model:      session.ModelRef{Provider: "openai", Model: model},
		Permission: "allow",
		MaxSteps:   12,
		System: "You are an orchestrator. You MUST delegate any essay/writing task to the 'worker' subagent " +
			"by calling the task tool with {\"agent\":\"worker\",\"prompt\":\"...\"}. Never write the essay yourself. " +
			"After delegating, tell the user briefly that you started, and answer any further quick questions immediately and directly.",
		Agents: map[string]AgentSpec{
			"worker": {Name: "worker", System: "You write long, detailed essays. Take your time and be thorough."},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: model}})
	if err != nil {
		t.Fatal(err)
	}
	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "Have the worker subagent write a long 400-word essay about the ocean."}},
	}); err != nil {
		t.Fatal(err)
	}

	var (
		spawned, steered, subDone bool
		respondedWhileRunning     bool
	)
	steer := func() {
		steered = true
		_ = a.Steer(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: "Quick question while that runs: what is 2+2? Answer in one short sentence."}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
	}

	for {
		select {
		case e, ok := <-sub:
			if !ok {
				goto check
			}
			switch e.Type {
			case event.TypeAgentSpawned:
				if !spawned {
					spawned = true
					steer() // inject a question while the subagent is running
				}
			case event.TypeAgentStatus:
				if strings.Contains(string(e.Data), `"state":"done"`) {
					subDone = true
				}
			case event.TypePartAppended:
				// An assistant text reply on the MAIN session after we steered and
				// before the subagent finished = the orchestrator stayed responsive.
				if steered && !subDone && strings.Contains(string(e.Data), `"role":"assistant"`) &&
					strings.Contains(string(e.Data), `"kind":"text"`) {
					respondedWhileRunning = true
				}
			case event.TypeTurnFinished:
				goto check
			case event.TypeError:
				t.Fatalf("loop error: %s", string(e.Data))
			}
		case <-ctx.Done():
			goto check
		}
	}

check:
	if !spawned {
		t.Skip("model did not delegate to a subagent; cannot exercise steering-during-subagent")
	}
	if !respondedWhileRunning {
		t.Fatal("main did not respond to the steered question while the subagent was still running (it blocked)")
	}
	t.Logf("OK: main responded to steer while subagent running (spawned=%v steered=%v subDone-before-response=%v)", spawned, steered, subDone)
}
