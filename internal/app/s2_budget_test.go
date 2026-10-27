package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type s2ArgsTool struct {
	metaTool
	args     *json.RawMessage
	deadline *time.Duration
}

func (t s2ArgsTool) Execute(ctx context.Context, args json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	*t.args = args
	if d, ok := ctx.Deadline(); ok {
		*t.deadline = time.Until(d)
	}
	return session.ToolResult{Content: json.RawMessage(`"comparison evidence"`)}, nil
}

// The automatic comparison's budget. Measured on aitoon: two of six runs ended with the baseline
// cut at 120s ("context deadline exceeded", 9 of 12 steps, 121s wall) — a document task that
// reads widely does not fit two minutes, and the candidate never ran. Each arm now has 240s, and
// the whole experiment (two arms plus two analyses) 10 minutes so the per-arm bound is the one
// that bites.
func TestS2HostAutoComparisonBudgetFitsTwoArms(t *testing.T) {
	a, s, _ := spawnApp(t, &fakeLLM{})
	var got json.RawMessage
	var left time.Duration
	a.tools.Register(s2ArgsTool{metaTool: metaTool{name: "s2_compare"}, args: &got, deadline: &left})
	actor := event.Actor{Kind: event.ActorAgent, ID: "default"}
	for i, st := range []port.ChildStep{
		{Name: "edit", Args: json.RawMessage(`{}`)},
		{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`)},
	} {
		id := fmt.Sprint(i)
		a.appendPart(context.Background(), s.ID, actor, "m"+id, session.RoleAssistant, session.Part{ID: "p" + id, Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: st.Name, Args: st.Args}})
		a.appendToolResult(context.Background(), s.ID, actor, "r"+id, id, "ok", false)
	}
	tc := turnCtx{s: s, agent: AgentSpec{}, actor: actor, guard: newRunGuard(a.touchesFile)}
	attempted, recalled := false, false
	a.maybeRunS2(context.Background(), tc, "revise the design document", &attempted, &recalled)
	var args struct {
		Timeout  int `json:"timeout"`
		MaxSteps int `json:"max_steps"`
	}
	if err := json.Unmarshal(got, &args); err != nil {
		t.Fatalf("no comparison dispatched: %q %v", got, err)
	}
	if args.Timeout != 240 || args.MaxSteps != 24 {
		t.Errorf("per-arm budget timeout=%d max_steps=%d, want 240 and 24", args.Timeout, args.MaxSteps)
	}
	if left < 2*240*time.Second {
		t.Errorf("experiment deadline %v cannot hold two %ds arms", left.Round(time.Second), args.Timeout)
	}
}
