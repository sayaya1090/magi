package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

// AGENTS.md in the workdir is injected into the system prompt (durable memory).
func TestProjectMemoryInSystemPrompt(t *testing.T) {
	wd := t.TempDir()
	os.WriteFile(filepath.Join(wd, "AGENTS.md"), []byte("Always use tabs, never spaces."), 0o644)

	llm := &usageLLM{text: "ok"}
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	runToTerminal(t, a, sid)

	sys := llm.lastSys()
	if !strings.Contains(sys, "Always use tabs") {
		t.Errorf("AGENTS.md not injected into system prompt; got: %q", sys)
	}
	if !strings.Contains(sys, "Project memory") {
		t.Errorf("memory header missing; got: %q", sys)
	}
}

// The loop publishes a context.usage meter event.
func TestContextUsagePublished(t *testing.T) {
	llm := &usageLLM{text: "ok"}
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	got := runToTerminal(t, a, sid)
	if countType(got, event.TypeContextUsage) == 0 {
		t.Errorf("expected a context.usage event, got %v", typesOf(got))
	}
}
