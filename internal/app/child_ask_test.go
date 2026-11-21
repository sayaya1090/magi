package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// childWriter opens a child it drives and writes a file there, a call that always asks the person.
type childWriter struct{ child string }

func (*childWriter) Name() string            { return "child_writer" }
func (*childWriter) Description() string     { return "writes a file in a child it drives" }
func (*childWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *childWriter) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	ch, err := env.OpenChild(ctx, "write one file")
	if err != nil {
		return session.ToolResult{Content: json.RawMessage(`"open failed"`), IsError: true}, nil
	}
	c.child = ch.SessionID
	r, err := ch.CallTool(ctx, "write", raw)
	failed := err
	if err == nil && r.IsError {
		failed = os.ErrPermission
	}
	ch.Close("wrote it", failed)
	return session.ToolResult{Content: json.RawMessage(`"done"`)}, nil
}

// A child the person cannot see is asked nothing they can answer: an ask raised inside a child a
// tool drives shows in the parent conversation (what the person is looking at), and the answer given
// there reaches the child. The parent's result names the child so its transcript can be opened.
func TestAnAskInADrivenChildIsAnsweredFromTheParent(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", Interactive: true, AnswerWait: 3 * time.Second})
	w := &childWriter{}
	a.tools.Register(w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := a.sessionInfo(ctx, sid)
	path := filepath.Join(s.Workdir, "made.txt")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "from-the-child"})
	outer := &session.ToolCall{CallID: "outer", Name: "child_writer", Args: args}
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	}()
	var ask Ask
	found := false
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end) && !found; time.Sleep(20 * time.Millisecond) {
		if ask, found = a.Waiting(sid); found && ask.What != "write" {
			found = false
		}
	}
	if !found {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("the child's ask never showed in the parent conversation")
	}
	if err := a.RespondPermission(ctx, command.RespondPermission{SessionID: sid, CallID: ask.ID, Decision: "allow"}); err != nil {
		t.Fatalf("an answer given in the parent must reach the child: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the child never went on after the answer")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "from-the-child" {
		t.Fatalf("the allowed write did not happen: %q %v", b, err)
	}
	if _, left := a.Waiting(sid); left {
		t.Fatal("the answered ask is still shown in the parent")
	}
	r := toolResultFor(t, a, sid, "outer")
	if r == nil || !strings.Contains(string(r.Content), "[child "+w.child) {
		t.Fatalf("the parent's result must name the child it drove: %v", r)
	}
}
