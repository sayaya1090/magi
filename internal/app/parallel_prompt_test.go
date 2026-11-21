package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// safetyProbe records what the host tells a script-running tool about running each inner tool
// alongside others.
type safetyProbe struct{ got map[string]bool }

func (*safetyProbe) Name() string            { return "safety_probe" }
func (*safetyProbe) Description() string     { return "reports which inner tools may overlap" }
func (*safetyProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p *safetyProbe) Execute(_ context.Context, _ json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	p.got = map[string]bool{}
	for _, name := range []string{"bash", "read", "write", "ask_user", "mcp__x__y"} {
		p.got[name] = env.ParallelToolSafe != nil && env.ParallelToolSafe(name)
	}
	return session.ToolResult{Content: json.RawMessage(`"ok"`)}, nil
}

// Branches of a script that do not depend on each other run together even when they are bash: the
// person is asked about them one at a time (see the test below), so the only thing that kept them
// apart is gone. A file writer, a question to the person and an MCP call still wait their turn.
func TestAScriptMayRunDangerToolsTogether(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	p := &safetyProbe{}
	a.tools.Register(p)
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	call := &session.ToolCall{CallID: "probe", Name: "safety_probe", Args: json.RawMessage(`{}`)}
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, call, newRunGuard(nil), "")
	want := map[string]bool{"bash": true, "read": true, "write": false, "ask_user": false, "mcp__x__y": false}
	for name, ok := range want {
		if p.got[name] != ok {
			t.Errorf("%s may overlap other inner calls: got %v, want %v", name, p.got[name], ok)
		}
	}
}

// One person, one prompt on screen: two calls of one conversation that both need asking are shown
// one after the other, never together, and both are decided.
func TestTwoPromptsForOneConversationAreShownOneAtATime(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	got := make(chan bool, 2)
	for _, id := range []string{"c1", "c2"} {
		tc := &session.ToolCall{CallID: id, Name: "bash", Args: json.RawMessage(`{"command":"true"}`)}
		go func() { got <- a.requestPermission(context.Background(), sid, actor, tc, true, "") }()
	}
	shown := func() int {
		a.mu.Lock()
		defer a.mu.Unlock()
		if st, ok := a.stateIf(sid); ok {
			return len(st.asking)
		}
		return 0
	}
	for answered := 0; answered < 2; answered++ {
		var ask Ask
		found := false
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end) && !found; time.Sleep(10 * time.Millisecond) {
			ask, found = a.Waiting(sid)
		}
		if !found {
			t.Fatalf("prompt %d never showed", answered+1)
		}
		time.Sleep(100 * time.Millisecond) // give a second prompt time to show, if it would
		if n := shown(); n != 1 {
			t.Fatalf("%d prompts on screen at once, want 1", n)
		}
		if err := a.RespondPermission(context.Background(), command.RespondPermission{SessionID: sid, CallID: ask.ID, Decision: "allow"}); err != nil {
			t.Fatal(err)
		}
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if w, ok := a.Waiting(sid); !ok || w.ID != ask.ID {
				break
			}
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case ok := <-got:
			if !ok {
				t.Fatal("an allowed prompt came back denied")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a prompt was never decided")
		}
	}
}
