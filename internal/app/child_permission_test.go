package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// runChildWrite drives a child that writes one file and reports whether the file was written and
// whether anyone was asked along the way.
func runChildWrite(t *testing.T, a *App, sid session.SessionID, name string) (written, asked bool) {
	t.Helper()
	a.tools.Register(&childWriter{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := a.sessionInfo(ctx, sid)
	path := filepath.Join(s.Workdir, name)
	args, _ := json.Marshal(map[string]string{"path": path, "content": "x"})
	outer := &session.ToolCall{CallID: "outer_" + name, Name: "child_writer", Args: args}
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, outer, newRunGuard(nil), "")
	}()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		select {
		case <-done:
			_, err := os.Stat(path)
			return err == nil, asked
		default:
		}
		if _, w := a.Waiting(sid); w {
			asked = true
			cancel()
		}
	}
	cancel()
	<-done
	_, err := os.Stat(path)
	return err == nil, asked
}

// A script runs for the conversation it was called from, so its writes follow that conversation's
// permission mode: in allow mode they run as the model's own would, with nobody asked.
func TestAScriptsWriteFollowsTheParentsAllowMode(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	if written, asked := runChildWrite(t, a, sid, "allowed.txt"); !written || asked {
		t.Fatalf("in allow mode a script's write must run like the model's own (written=%v asked=%v)", written, asked)
	}
}

// A standing "always" the person gave in the conversation covers the scripts it runs too.
func TestAScriptsWriteUsesTheParentsStandingGrant(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", Interactive: true, AnswerWait: 3 * time.Second})
	a.mu.Lock()
	st := a.stateLocked(sid)
	if st.grants == nil {
		st.grants = map[string]bool{}
	}
	st.grants["write"] = true
	a.mu.Unlock()
	if written, asked := runChildWrite(t, a, sid, "granted.txt"); !written || asked {
		t.Fatalf("a write the conversation already allowed for good must not be asked again in its script (written=%v asked=%v)", written, asked)
	}
}

// And in ask mode with no grant it is still asked, in the conversation (relayed), not refused.
func TestAScriptsWriteInAskModeIsAskedInTheParent(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", Interactive: true, AnswerWait: 3 * time.Second})
	if _, asked := runChildWrite(t, a, sid, "asked.txt"); !asked {
		t.Fatal("in ask mode a script's write must be asked in the conversation")
	}
}
