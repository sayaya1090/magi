package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A mid-turn message that stays QUEUED runs as its OWN top-level turn once the current
// one finishes. It must be judged on its own merits — NOT held to the previous task's
// plan/criteria. Regression: the drain path re-submitted the queued prompt via
// appendPrompt, bypassing Submit's per-task reset, so the termination council judged an
// unrelated queued request ("what is 2+2?") against the finished task's leftover todo
// plan ("이전 맥락으로 판정"). resetForNewTopLevel now runs on the drain too.
func TestQueuedInterjectionGetsFreshTaskContract(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	bt := &blockingTool{started: started, release: release}
	reg := builtin.Default()
	reg.Register(bt)

	// The unrelated question is steered in as WORK (routeAside → true) so finish-boundary triage
	// escalates it to its own turn 2 — where its fresh-contract judging is asserted. Triage turns
	// don't advance the positional script below.
	llm := &triageAwareLLM{routeAside: func(string) bool { return true }, steps: [][]port.ProviderEvent{
		// Turn 1 step 0: set a plan with a leftover PENDING item, then block (hold turn open).
		{
			{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_todo", Name: "todowrite", Args: json.RawMessage(`{"todos":[{"content":"TASK1-LEFTOVER-STEP","status":"pending"}]}`)}},
			{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_block", Name: "block", Args: json.RawMessage(`{}`)}},
			{Type: port.ProviderFinish},
		},
		// Turn 1 step 1: finish → council for turn 1.
		textStep("task 1 done"),
		// Turn 2 (drained interjection) step 0: a read so the gate fires, then finish.
		toolStep("read", `{"path":"x"}`),
		textStep("2 plus 2 is 4"),
	}}

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Council: fc})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "review the whole project"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	<-started // turn 1 is blocked mid-step with a plan set

	// An UNRELATED question is steered in → queued to run as its own turn.
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "UNRELATED-QUESTION what is 2+2?"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	close(release) // turn 1 continues → finishes → drain runs the interjection as turn 2

	// Wait until the run goroutine retires and the queue drains.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		idle := a.cancels[sid] == nil && len(a.pendingInterject[sid]) == 0
		a.mu.Unlock()
		if idle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	req := fc.lastReq // the LAST deliberation = turn 2 (the drained interjection)
	if !strings.Contains(req.Task, "UNRELATED-QUESTION") {
		t.Fatalf("turn 2 council should judge the interjection, got Task=%q", req.Task)
	}
	if strings.Contains(req.Plan, "TASK1-LEFTOVER-STEP") {
		t.Fatalf("queued interjection was judged against the previous task's plan: Plan=%q", req.Plan)
	}
}
