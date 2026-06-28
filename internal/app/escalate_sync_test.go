package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A synchronously-spawned subagent (a planner explorer or a nested subagent) has no
// orchestrator loop to answer an `ask` — its parent is blocked awaiting it — so the
// tool must fail FAST with guidance instead of blocking until the 2-minute
// escalation timeout. Regression for the hang where a scout explorer's ask stalled
// the whole turn for two minutes.
func TestSyncSpawnAskFailsFast(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("ask", `{"question":"which log directory?"}`),
		textStep("proceeding with best assumption"),
	}}
	reg := builtin.Default()
	reg.Register(builtin.Ask{}) // ask isn't in the default set; the real registry adds it
	a := New(store, llm, reg, bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"explorer": {Name: "explorer"}},
		// A short subagent timeout bounds the FAILURE mode: without the fix, escalate()
		// would block until this fires (then retry), so a fast return well under it
		// proves the ask short-circuited.
		SubagentTimeout: 5 * time.Second,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	start := time.Now()
	// Background defaults to false → child is not Escalatable → ask must fail fast.
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "explorer", Prompt: "investigate"})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ask blocked %v — a synchronous spawn must fail fast (the fix returns in ms; "+
			"a regression blocks until the %v subagent timeout)", elapsed, 5*time.Second)
	}
	if res.Err != "" {
		t.Fatalf("spawn errored: %s", res.Err)
	}
	if !strings.Contains(res.Text, "proceeding") {
		t.Fatalf("subagent should continue after the fast ask failure, got %q", res.Text)
	}
	// The ask result carried the no-orchestrator guidance (not an escalation timeout).
	if !childSessionContains(t, a, "s_parent", "no orchestrator is available") {
		t.Error("ask result should carry the no-orchestrator guidance message")
	}
}

// The other half of the branch: a background-dispatched child IS Escalatable, so
// its `ask` routes through escalate() and is answered by the orchestrator (here the
// test plays the orchestrator by answering the pending ask) — not fast-failed.
func TestBackgroundSpawnAskEscalates(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("ask", `{"question":"which log directory?"}`),
		textStep("got it"),
	}}
	reg := builtin.Default()
	reg.Register(builtin.Ask{})
	a := New(store, llm, reg, bus.New(), nil, Config{
		Permission:      "allow",
		Agents:          map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentTimeout: 10 * time.Second,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	// escalate() appends the question to the PARENT session, so it must exist in the
	// store — create it properly (unlike the fast-fail case, which never touches it).
	parentID, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	parent := a.sessionInfo(context.Background(), parentID)

	// Stand in for the orchestrator loop: answer the escalation once it registers.
	go func() {
		for i := 0; i < 400; i++ {
			a.mu.Lock()
			pending := a.pendingAsks[parentID] != nil
			a.mu.Unlock()
			if pending {
				a.answerPendingAsk(parentID, "use ./logs")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Background:true → Escalatable → askFn routes through escalate().
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go", Background: true})
	if res.Err != "" {
		t.Fatalf("spawn errored: %s", res.Err)
	}
	if !strings.Contains(res.Text, "got it") {
		t.Fatalf("subagent should continue after the answer, got %q", res.Text)
	}
	if childSessionContains(t, a, parentID, "no orchestrator is available") {
		t.Error("a background-dispatched subagent must escalate, not fast-fail")
	}
	if !childSessionContains(t, a, parentID, "use ./logs") {
		t.Error("the orchestrator's answer should reach the subagent as the ask result")
	}
}

// childSessionContains reports whether any event in any child session of parent
// contains needle (scans the raw event data).
func childSessionContains(t *testing.T, a *App, parent session.SessionID, needle string) bool {
	t.Helper()
	a.mu.Lock()
	var kids []session.SessionID
	for id, s := range a.sessions {
		if s.Parent == parent {
			kids = append(kids, id)
		}
	}
	a.mu.Unlock()
	for _, id := range kids {
		evs, _ := a.store.Read(context.Background(), id, 0)
		for _, e := range evs {
			if strings.Contains(string(e.Data), needle) {
				return true
			}
		}
	}
	return false
}
