package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestS2HostTriggerUsesObservedEditAndExecution(t *testing.T) {
	check := port.ChildStep{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Failed: true, Output: "test parser failed"}
	edit := port.ChildStep{Name: "edit"}
	for _, tt := range []struct {
		name  string
		steps []port.ChildStep
		want  bool
	}{
		{"retry after edit", []port.ChildStep{check, edit, check}, true},
		{"single failure", []port.ChildStep{check}, false},
		{"blind retry", []port.ChildStep{check, check}, false},
		{"no check", []port.ChildStep{edit}, false},
		{"successful edit and check", []port.ChildStep{edit, {Name: "bash", Args: check.Args}}, true},
		{"already compared", []port.ChildStep{check, edit, check, {Name: "s2_compare"}}, false},
		{"permission refusal", []port.ChildStep{{Name: "bash", Args: check.Args, Failed: true, Output: "blocked by policy"}, edit, check}, false},
		// macOS says EPERM as "Operation not permitted" — a sandbox refusing a lock file, not a check
		// that failed. Measured: aitoon's `git add` hit `.git/index.lock: Operation not permitted`.
		{"sandbox EPERM", []port.ChildStep{edit, {Name: "bash", Args: check.Args, Failed: true, Output: "fatal: Unable to create '.git/index.lock': Operation not permitted"}}, false},
		{"sandbox EPERM retried", []port.ChildStep{{Name: "bash", Args: check.Args, Failed: true, Output: "Operation not permitted"}, edit, {Name: "bash", Args: check.Args, Failed: true, Output: "Operation not permitted"}}, false},
		{"different command", []port.ChildStep{check, edit, {Name: "bash", Args: json.RawMessage(`{"command":"go vet ./..."}`), Failed: true}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := s2Trigger(tt.steps); got != tt.want {
				t.Fatalf("trigger=%v want %v", got, tt.want)
			}
		})
	}
}

type s2ObservedTool struct {
	metaTool
	calls *int
}

func (t s2ObservedTool) Execute(_ context.Context, _ json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	*t.calls++
	return session.ToolResult{Content: json.RawMessage(`"comparison evidence"`)}, nil
}

func TestS2HostDispatchesWithoutModelRequestAndOnlyOnce(t *testing.T) {
	a, s, _ := spawnApp(t, &fakeLLM{})
	count := 0
	a.tools.Register(s2ObservedTool{metaTool: metaTool{name: "s2_compare"}, calls: &count})
	actor := event.Actor{Kind: event.ActorAgent, ID: "default"}
	for i, st := range []port.ChildStep{
		{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Failed: true},
		{Name: "edit", Args: json.RawMessage(`{}`)},
		{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Failed: true},
	} {
		id := fmt.Sprint(i)
		a.appendPart(context.Background(), s.ID, actor, "m"+id, session.RoleAssistant, session.Part{ID: "p" + id, Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: st.Name, Args: st.Args}})
		a.appendToolResult(context.Background(), s.ID, actor, "r"+id, id, "test output", st.Failed)
	}
	tc := turnCtx{s: s, agent: AgentSpec{}, actor: actor, guard: newRunGuard(a.touchesFile)}
	attempted, recalled := false, false
	a.maybeRunS2(context.Background(), tc, "fix tests", &attempted, &recalled)
	a.maybeRunS2(context.Background(), tc, "fix tests", &attempted, &recalled)
	if count != 1 || !attempted {
		t.Fatalf("host dispatched %d comparisons, attempted=%v", count, attempted)
	}
	steps, err := a.turnSteps(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, st := range steps {
		if st.Name == "s2_compare" {
			found = true
		}
	}
	if !found {
		t.Fatal("host comparison missing from durable tool evidence")
	}
}

// The task model emits only write/bash/answer. S² must be scheduled by runLoop.
func TestS2HostLoopSchedulesAfterOrdinaryTaskExecution(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "write", Name: "write", Args: json.RawMessage(`{"path":"answer.txt","content":"answer"}`)}}, {Type: port.ProviderFinish}},
		{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "check", Name: "bash", Args: json.RawMessage(`{"command":"test -f answer.txt"}`)}}, {Type: port.ProviderFinish}},
		{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}},
	}}
	a, s, _ := spawnApp(t, llm)
	a.notePromptText(context.Background(), s.ID, event.Actor{Kind: event.ActorUser, ID: "user"}, "write answer.txt and check that it exists")
	count := 0
	a.tools.Register(s2ObservedTool{metaTool: metaTool{name: "s2_compare"}, calls: &count})
	_, err := a.runLoop(context.Background(), s, AgentSpec{}, 0, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ordinary model task caused %d host comparisons; want 1", count)
	}
}

func TestS2HostDoesNotEnterChildTrialOrCancelledTurn(t *testing.T) {
	for _, mode := range []string{"child", "trial", "cancelled", "restricted"} {
		t.Run(mode, func(t *testing.T) {
			a, s, _ := spawnApp(t, &fakeLLM{})
			count := 0
			a.tools.Register(s2ObservedTool{metaTool: metaTool{name: "s2_compare"}, calls: &count})
			tc := turnCtx{s: s, agent: AgentSpec{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "child":
				tc.depth = 1
			case "trial":
				tc.agent.Trial = true
			case "cancelled":
				cancel()
			case "restricted":
				tc.agent.Tools = []string{"read"}
			}
			attempted, recalled := false, false
			a.maybeRunS2(ctx, tc, "task", &attempted, &recalled)
			if count != 0 || attempted || recalled {
				t.Fatal("ineligible turn entered S²")
			}
		})
	}
}
