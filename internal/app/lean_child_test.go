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
	"github.com/sayaya1090/magi/internal/port"
)

// A lean child gets its own instructions, the environment and the project memory — the rules a
// reader may need ("ignore vendor/") — but not the guides written for the main conversation:
// output formatting, companion ownership and the coding workflow. A child that only looks spends
// its tokens on reading, not on rules it will never act on.
func TestALeanChildLeavesTheMainConversationsGuidesOut(t *testing.T) {
	systemOf := func(lean bool) string {
		t.Helper()
		store, _ := jsonl.New(t.TempDir())
		llm := &usageLLM{text: "found"}
		a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
		wd := t.TempDir()
		if err := os.WriteFile(filepath.Join(wd, "AGENTS.md"), []byte("MEMORY-RULE: ignore vendor/"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
		res, err := a.spawnChild(ctx, a.sessionInfo(ctx, sid), event.Actor{Kind: event.ActorAgent, ID: "default"},
			port.SpawnSpec{System: "CHILD-OWN", Prompt: "where?", Tools: []string{"read"}, Lean: lean}, nil)
		if err != nil || res.Err != "" {
			t.Fatalf("spawn: %v %q", err, res.Err)
		}
		return llm.lastSys()
	}
	lean := systemOf(true)
	for _, keep := range []string{"CHILD-OWN", "MEMORY-RULE"} {
		if !strings.Contains(lean, keep) {
			t.Errorf("a lean child lost %q:\n%s", keep, lean)
		}
	}
	for _, gone := range []string{strings.TrimSpace(outputFormatGuide)[:30], strings.TrimSpace(companionOwnershipGuide)[:30]} {
		if strings.Contains(lean, gone) {
			t.Errorf("a lean child still carries %q", gone)
		}
	}
	if full := systemOf(false); !strings.Contains(full, strings.TrimSpace(outputFormatGuide)[:30]) {
		t.Error("a child that is not lean keeps every guide, as before")
	}
}
