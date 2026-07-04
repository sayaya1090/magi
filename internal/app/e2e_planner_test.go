package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	councilllm "github.com/sayaya1090/magi/internal/adapter/council/llm"
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

// e2eExplorers is the minimal read-only explorer set the procedure planner can
// dispatch (mirrors cmd/magi defaultAgents, which lives in package main).
func e2eExplorers() map[string]AgentSpec {
	ro := []string{"read", "grep", "glob", "list"}
	return map[string]AgentSpec{
		"explore": {Name: "explore", System: "You are a read-only explorer. Investigate with read/grep/glob/list and report concise findings. Never modify files.", Tools: ro},
		"locator": {Name: "locator", System: "You locate files/symbols with grep/glob/list/read. Never modify files.", Tools: ro},
		"analyst": {Name: "analyst", System: "You analyze with read/grep/glob/list. Never modify files.", Tools: ro},
		"planner": {Name: "planner", System: "You are a procedure planner. Lay out the ordered procedure (steps with strategy solo|parallel|scout) to handle the request. Read-only explorers are explore|locator|analyst. Plan how to investigate, not how to code.", Tools: ro},
	}
}

// TestE2EProcedurePlanner drives the procedure planner (D17) against a real model:
// the planner decomposes the request, the council audits the plan, and read-only
// explorers gather findings — all before the main turn. Skipped when no backend is
// reachable. It asserts the turn completes and logs the procedure/audit so the
// adaptive scout→fanout and plan-audit behaviour is visible.
func TestE2EProcedurePlanner(t *testing.T) {
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

	llm := openai.New(base, os.Getenv("MAGI_E2E_API_KEY"))
	council := councilllm.New(func(string) port.LLMProvider { return llm }, model)

	// A repo with several docs so a "summarize the docs" request invites a
	// scout (list docs) → fan-out (read each) procedure.
	wd := t.TempDir()
	docs := filepath.Join(wd, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"ARCHITECTURE.md": "# Architecture\nHexagonal: core, ports, adapters.\n",
		"DESIGN.md":       "# Design\nEvent-sourced JSONL store; CQRS-lite.\n",
		"PLAN.md":         "# Plan\nMilestones and roadmap.\n",
		"SPEC.md":         "# Spec\nFeature rules and acceptance examples.\n",
	} {
		if err := os.WriteFile(filepath.Join(docs, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "e2e@magi.test"}, {"config", "user.name", "magi"}, {"add", "-A"}, {"commit", "-q", "-m", "seed"}} {
		c := exec.Command("git", args...)
		c.Dir = wd
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), platform.New(), Config{
		Model:            session.ModelRef{Provider: "openai", Model: model},
		System:           "You are a coding agent in a working directory.",
		Permission:       "allow",
		MaxSteps:         40,
		Planner:          true,
		Agents:           e2eExplorers(),
		Council:          council,
		CouncilMaxRounds: 2,
	})

	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: model}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// Seed a user prompt WITHOUT starting the main loop, then drive the planner
	// directly. This isolates D17 (decompose → plan audit → scout/fanout → inject)
	// from the main turn and its termination gate — which on a read-only task can
	// churn independently of the planner.
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1",
		Parts:     []session.Part{{Kind: session.PartText, Text: "List the markdown files under docs/, then read each one and summarize what it documents."}},
	})
	if err := a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "u"}, pd); err != nil {
		t.Fatal(err)
	}

	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var planPhase, planAudit, planDecided, injected int
	collected := make(chan struct{})
	go func() {
		for e := range sub {
			switch e.Type {
			case event.TypeWorkflowPhase:
				var d event.WorkflowPhaseData
				if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
					planPhase++
					t.Logf("PLAN phase: status=%q detail=%q", d.Status, d.Detail)
				}
			case event.TypeCouncilConvened:
				var d event.CouncilConvenedData
				if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
					planAudit++
					t.Logf("plan audit round %d (%s): %v", d.Round, d.Rule, d.Members)
				}
			case event.TypeCouncilVerdict:
				var d event.CouncilVerdictData
				if json.Unmarshal(e.Data, &d) == nil {
					t.Logf("  ↳ %s [%s] %s — %s", d.Member, d.Lens, d.Decision, d.Rationale)
				}
			case event.TypeCouncilDecided:
				var d event.CouncilDecidedData
				if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
					planDecided++
					t.Logf("plan audit DECIDED %s %s", d.Decision, d.Note)
				}
			case event.TypePromptSubmitted:
				if e.Actor.ID == "planner" {
					injected++
					t.Logf("planner injected findings into the session")
				}
			}
		}
		close(collected)
	}()

	// Synchronous: planner decompose → (multi-step) plan audit → execute steps
	// (scout/fanout explorers) → inject findings. No main turn.
	a.maybePlanPreflight(ctx, a.sessionInfo(ctx, sid), 0)
	cancelSub()
	<-collected

	if planPhase == 0 {
		t.Error("the procedure planner did not emit a plan phase (it did not run)")
	}
	// If the planner produced a multi-step plan, the audit must convene AND decide
	// (never hang). A single-step plan legitimately skips the audit.
	if planAudit > 0 && planDecided == 0 {
		t.Errorf("plan audit convened (%d) but never decided", planAudit)
	}
	t.Logf("summary: planPhase=%d planAudit=%d planDecided=%d injected=%d", planPhase, planAudit, planDecided, injected)
}
