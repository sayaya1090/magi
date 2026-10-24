package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

func TestTrialsUseOneSnapshotDespiteParentAndSiblingEdits(t *testing.T) {
	llm := &usageLLM{text: "finished"}
	a, parent, _ := spawnApp(t, llm)
	parent.Workdir = gitRepo(t)
	path := filepath.Join(parent.Workdir, "input.txt")
	if err := os.WriteFile(path, []byte("original uncommitted input"), 0600); err != nil {
		t.Fatal(err)
	}
	putMemo(t, filepath.Join(parent.Workdir, "AGENTS.md"), "original instructions")
	hooks := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent}, "trial-call", "experiment")
	defer hooks.Close()
	spec := port.SpawnSpec{Trial: true, System: "compare", Prompt: "inspect input.txt", Tools: []string{"read"}, MaxSteps: 4}
	first, err := hooks.Spawn(context.Background(), spec)
	if err != nil || first.Err != "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(first.Workspace)) })
	for _, p := range []string{path, filepath.Join(first.Workspace, "input.txt")} {
		if err := os.WriteFile(p, []byte("changed after baseline"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	putMemo(t, filepath.Join(parent.Workdir, "AGENTS.md"), "changed instructions")
	second, err := hooks.Spawn(context.Background(), spec)
	if err != nil || second.Err != "" {
		t.Fatalf("second: %+v %v", second, err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(second.Workspace)) })
	b, err := os.ReadFile(filepath.Join(second.Workspace, "input.txt"))
	if err != nil || string(b) != "original uncommitted input" {
		t.Fatalf("trial source drifted: %q %v", b, err)
	}
	if !first.Trial || !second.Trial || first.Snapshot == "" || first.Snapshot != second.Snapshot || first.Workspace == second.Workspace {
		t.Fatalf("snapshot/checkout identity: %+v %+v", first, second)
	}
	if sys := llm.lastSys(); !strings.Contains(sys, "original instructions") || strings.Contains(sys, "changed instructions") {
		t.Fatalf("instructions changed between trial executions: %s", sys)
	}
	if err := hooks.Merge(context.Background(), second.SessionID); err == nil || !strings.Contains(err.Error(), "evidence only") {
		t.Fatalf("trial merged: %v", err)
	}
}

func TestTrialRefusesLiveToolsAndMissingAllowlist(t *testing.T) {
	for _, tools := range [][]string{nil, {"read", "remember"}, {"mcp__app__write"}, {"skill"}} {
		if err := validateTrial(port.SpawnSpec{Tools: tools}); err == nil {
			t.Fatalf("trial accepted tools %v", tools)
		}
	}
}

type countingTrialContext struct{ calls int }

func (p *countingTrialContext) Provide(context.Context, port.ContextQuery) ([]port.ContextChunk, error) {
	p.calls++
	return []port.ContextChunk{{Text: "live memory from another run"}}, nil
}

func TestTrialDoesNotRetrieveLivePluginContext(t *testing.T) {
	a, s, _ := spawnApp(t, &usageLLM{text: "done"})
	p := &countingTrialContext{}
	a.RegisterContextProvider(p)
	agent := AgentSpec{Name: "trial", Trial: true, Tools: []string{"read"}}
	got := a.volatileContext(context.Background(), s, agent, nil, nil, 1, 5, time.Second, map[string]bool{"read": true})
	if p.calls != 0 || strings.Contains(got, "live memory") {
		t.Fatalf("live provider reached trial: calls=%d %s", p.calls, got)
	}
}
