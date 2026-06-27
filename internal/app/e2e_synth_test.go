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
)

// TestE2EReviewSynthesizeTerminates reproduces the user's scenario — ask the
// orchestrator to have coder + tester review a doc and synthesize — and asserts
// it actually FINISHES (the synthesis nudge stops the weak model from looping
// re-reads/echoes until max steps). Run against a live Ollama.
func TestE2EReviewSynthesizeTerminates(t *testing.T) {
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
	if err := os.WriteFile(wd+"/DESIGN.md", []byte("# Design\n\nA CQRS-lite agent with ports/adapters. Core has no deps.\n"), 0o644); err != nil {
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
		System: "You are an orchestrator. Delegate review work to the 'coder' and 'tester' subagents via the task " +
			"tool, then synthesize their results. Subagent results arrive as messages; once all have reported, write " +
			"one concise synthesis and STOP — do not re-read files or re-dispatch.",
		Agents: map[string]AgentSpec{
			"coder":  {Name: "coder", System: "Briefly review the design from a coding perspective in 3 bullets.", Tools: []string{"read", "grep", "glob", "list", "ask", "report"}},
			"tester": {Name: "tester", System: "Briefly review the design from a testing perspective in 3 bullets.", Tools: []string{"read", "grep", "glob", "list", "ask", "report"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
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
		Parts:     []session.Part{{Kind: session.PartText, Text: "Have the coder and tester subagents review DESIGN.md, then synthesize their feedback."}},
	})

	var spawns, toolCalls int
	var trace []string
	finished := false
	for !finished {
		select {
		case e, ok := <-sub:
			if !ok {
				goto check
			}
			trace = append(trace, traceLine(e))
			switch e.Type {
			case event.TypeAgentSpawned:
				spawns++
			case event.TypePartAppended:
				if strings.Contains(string(e.Data), `"kind":"tool-call"`) {
					toolCalls++
				}
			case event.TypeTurnFinished:
				finished = true
			case event.TypeError:
				// "max steps reached" = the loop bug; fail loudly.
				t.Fatalf("turn ended in error (likely the infinite-loop hitting max steps): %s", string(e.Data))
			}
		case <-ctx.Done():
			goto check
		}
	}

check:
	if !finished {
		from := 0
		if len(trace) > 50 {
			from = len(trace) - 50
		}
		t.Fatalf("turn did not finish (timed out — likely looping). spawns=%d toolCalls=%d\nlast events:\n%s",
			spawns, toolCalls, strings.Join(trace[from:], "\n"))
	}
	t.Logf("OK: finished. subagent spawns=%d, tool calls=%d", spawns, toolCalls)
	// Sanity: it shouldn't have spawned a runaway number of subagents.
	if spawns > 6 {
		t.Fatalf("too many subagent spawns (%d) — likely re-dispatch loop", spawns)
	}
}

// traceLine renders a one-line summary of an orchestrator event for diagnostics.
func traceLine(e event.Event) string {
	s := string(e.Type) + " [" + string(e.Actor.Kind) + ":" + e.Actor.ID + "]"
	switch e.Type {
	case event.TypePartAppended:
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil {
			switch d.Part.Kind {
			case session.PartToolCall:
				if d.Part.ToolCall != nil {
					s += " tool-call=" + d.Part.ToolCall.Name + " args=" + clip(string(d.Part.ToolCall.Args), 80)
				}
			case session.PartText:
				s += " text=" + clip(d.Part.Text, 80)
			}
		}
	case event.TypePromptSubmitted:
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil {
			s += " prompt=" + clip(joinParts(d.Parts), 80)
		}
	}
	return s
}

func joinParts(ps []session.Part) string {
	var b strings.Builder
	for _, p := range ps {
		if p.Kind == session.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
