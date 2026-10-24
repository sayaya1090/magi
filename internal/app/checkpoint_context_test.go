package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestCheckpointCopiesExactHistoryAndContinuesOnlyChosenBranch(t *testing.T) {
	ctx := context.Background()
	llm := &usageLLM{text: "baseline-only-sentinel"}
	a, s, _ := spawnApp(t, llm)
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	history := []session.Message{
		{ID: "folded", Role: session.RoleSystem, Parts: []session.Part{{Kind: session.PartText, Text: "already compacted before checkpoint; retain this exact text"}}},
		{ID: "request", Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "원문 그대로\nretain punctuation, order and whitespace.  "}}},
		{ID: "call", Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "old-call", Name: "read", Args: json.RawMessage(`{"path":"input"}`)}}}},
		{ID: "result", Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "old-call", Content: json.RawMessage(`"full output\n두 번째 줄  "`)}}}},
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "# Runtime context\nexact live plan at checkpoint"}}},
	}
	encoded, _ := json.Marshal(history)
	a.mu.Lock()
	a.stateLocked(s.ID).lastRequestContext = encoded
	a.stateLocked(s.ID).lastRequestSystem = "original system bytes"
	a.mu.Unlock()
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "directory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	var selected string
	for i := 0; i < 2; i++ {
		if i == 1 {
			llm.say("candidate-only-sentinel")
			if err := a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorUser}, "arrived after checkpoint; must not enter either branch"); err != nil {
				t.Fatal(err)
			}
		}
		fork, err := w.action(ctx, port.CheckpointRequest{Action: "fork"})
		if err != nil {
			t.Fatal(err)
		}
		hooks := a.spawnFnFor(0, s, event.Actor{Kind: event.ActorAgent}, newID(), "experiment")
		r, err := hooks.Spawn(ctx, port.SpawnSpec{Trial: true, CheckpointBranch: fork.Branch, Prompt: "same task", System: "branch rules", Tools: []string{"read"}, MaxSteps: 4})
		hooks.Close()
		if err != nil || r.Err != "" {
			t.Fatalf("spawn: %+v %v", r, err)
		}
		request := llm.lastRequest()
		if len(request) < len(history) || !reflect.DeepEqual(request[:len(history)], history) {
			t.Fatalf("checkpoint history changed: %#v", request)
		}
		if !strings.Contains(llm.lastSys(), "original system bytes") {
			t.Fatal("inherited system missing")
		}
		if i == 1 {
			selected = fork.Branch
		}
	}
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: selected}); err != nil {
		t.Fatal(err)
	}
	next, err := w.action(ctx, port.CheckpointRequest{Action: "fork"})
	if err != nil {
		t.Fatal(err)
	}
	llm.say("next execution")
	hooks := a.spawnFnFor(0, s, event.Actor{Kind: event.ActorAgent}, newID(), "experiment")
	defer hooks.Close()
	if _, err := hooks.Spawn(ctx, port.SpawnSpec{Trial: true, CheckpointBranch: next.Branch, Prompt: "next task", Tools: []string{"read"}, MaxSteps: 4}); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(llm.lastRequest())
	if strings.Contains(string(request), "baseline-only-sentinel") || !strings.Contains(string(request), "candidate-only-sentinel") {
		t.Fatalf("wrong branch lineage: %s", request)
	}
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "finish"}); err != nil {
		t.Fatal(err)
	}
	if err := a.finishCheckpoints(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	evs, err := a.store.Read(ctx, s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := json.Marshal(reconstruct(evs))
	if strings.Contains(string(parent), "baseline-only-sentinel") || !strings.Contains(string(parent), "candidate-only-sentinel") {
		t.Fatalf("disposal lost selected context: %s", parent)
	}
}

func TestCheckpointContextBoundaryAndImageLifetime(t *testing.T) {
	complete := session.Message{ID: "complete", Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "keep"}}}
	pending := session.Message{ID: "pending", Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "checkpoint-call"}}}}
	if got := checkpointHistoryBoundary([]session.Message{complete, pending}); !reflect.DeepEqual(got, []session.Message{complete}) {
		t.Fatal("in-flight call inherited without result")
	}
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	image := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(image, []byte("image bytes"), 0600)
	source := []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartImage, Image: &session.ImageRef{Path: image, MIME: "image/png"}}}}}
	encoded, _ := json.Marshal(source)
	a.mu.Lock()
	a.stateLocked(s.ID).lastRequestContext = encoded
	a.mu.Unlock()
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "directory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	pinned := w.context[0].Parts[0].Image.Path
	os.Remove(image)
	if pinned == image {
		t.Fatal("ephemeral attachment not pinned")
	}
	if err := w.cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(pinned)
	if err != nil || string(b) != "image bytes" {
		t.Fatalf("context attachment deleted with runtime: %s %v", b, err)
	}
	if a.maybeCompact(ctx, s, AgentSpec{preserveContext: true}, event.Actor{}, nil, w.context, strings.Repeat("large", 10000)) {
		t.Fatal("checkpoint context was independently summarized")
	}
}
