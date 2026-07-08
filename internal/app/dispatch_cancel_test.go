package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// cancel_dispatch lets the orchestrator drop the remaining parallel subagents once an
// intermediate result made them unnecessary. These tests pin the three behaviors the
// feature promises: (1) it refuses when no intermediate result has arrived yet (abuse
// guard), (2) it cancels a running background child of THIS orchestrator, and (3) the
// cancelled child's injected result is an honest "cancelled by orchestrator" notice with
// a compensation manifest — nothing is auto-rolled back (R0).

// registerRunningChild seeds a background child session of parent with a live cancel, as
// spawn() would, and returns a channel closed when its context is cancelled.
func registerRunningChild(a *App, parent, child session.SessionID, agent, wd string) <-chan struct{} {
	sess := session.Session{ID: child, Parent: parent, Escalatable: true, Agent: agent, Workdir: wd}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { <-ctx.Done(); close(done) }()
	a.mu.Lock()
	a.sessions[child] = sess
	a.cancels[child] = cancel
	a.mu.Unlock()
	// Initialize the child's event log so later appends (and reads) resolve, as spawn does.
	cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: agent})
	_ = a.appendFact(context.Background(), child, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorAgent, ID: agent}, cd)
	return done
}

func TestDispatchCancelAbuseGate(t *testing.T) {
	a, wd := newApp(t, workingLLM(), Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	// A wave is running but NO result has come back yet: cancel must be refused.
	registerRunningChild(a, sid, "s_child_a", "coder", wd)
	a.mu.Lock()
	g := a.bgFor(sid)
	g.outstanding = 1
	a.mu.Unlock()

	n, err := a.cancelDispatched(ctx, sid, "", "changed my mind")
	if err == nil {
		t.Fatalf("cancel should be refused before any intermediate result, got n=%d err=nil", n)
	}
	if !strings.Contains(err.Error(), "intermediate result") {
		t.Fatalf("gate error should explain the missing intermediate result, got %q", err.Error())
	}
	if n != 0 {
		t.Fatalf("nothing should be cancelled when the gate refuses, got n=%d", n)
	}

	// An empty reason is also refused (intent must be recorded).
	a.mu.Lock()
	g.completed = map[string]bool{"coder\x00x": true} // pretend one already finished
	a.mu.Unlock()
	if _, err := a.cancelDispatched(ctx, sid, "", "   "); err == nil {
		t.Fatal("cancel with a blank reason should be refused")
	}
}

func TestDispatchCancelCancelsAndReportsHonestly(t *testing.T) {
	a, wd := newApp(t, workingLLM(), Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	// One sibling already produced a result (gate satisfied); another is still running.
	done := registerRunningChild(a, sid, "s_child_b", "tester", wd)
	a.mu.Lock()
	g := a.bgFor(sid)
	g.outstanding = 2
	g.completed = map[string]bool{"coder\x00do X": true}
	g.injected = 1
	a.mu.Unlock()

	// Seed an action into the child's log so the compensation manifest is non-empty.
	childActor := event.Actor{Kind: event.ActorAgent, ID: "tester"}
	a.appendPart(ctx, "s_child_b", childActor, "m_1", session.RoleAssistant, session.Part{
		Kind:     session.PartToolCall,
		ToolCall: &session.ToolCall{CallID: "c_bash", Name: "bash"},
	})
	a.appendToolResult(ctx, "s_child_b", childActor, "m_1", "c_bash", "created /tmp/scratch/output.bin", false)

	n, err := a.cancelDispatched(ctx, sid, "", "the fast sibling already answered the question")
	if err != nil {
		t.Fatalf("cancel should succeed once a result has arrived: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 subagent cancelled, got %d", n)
	}

	// The child's context was actually cancelled.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the running child was not cancelled")
	}

	// The reason was recorded so injectSubagentResult renders the honest notice.
	a.mu.Lock()
	reason, marked := a.bg[sid].cancelled["s_child_b"]
	a.mu.Unlock()
	if !marked || reason == "" {
		t.Fatalf("cancelled child should be marked with its reason, got marked=%v reason=%q", marked, reason)
	}

	// Now inject the cancelled result as the spawn goroutine would, and assert the parent
	// sees an honest cancel + compensation notice carrying the action manifest.
	a.injectSubagentResult(ctx, sid, "tester", port.SpawnResult{SessionID: "s_child_b", Err: "context canceled"})
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := lastPromptText(evs)
	for _, want := range []string{"cancelled by orchestrator", "the fast sibling already answered", "compensating", "bash", "output.bin"} {
		if !strings.Contains(last, want) {
			t.Fatalf("cancel notice missing %q; got:\n%s", want, last)
		}
	}
}

// lastPromptText returns the text of the most recent prompt.submitted event.
func lastPromptText(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(evs[i].Data, &d) != nil {
			continue
		}
		var b strings.Builder
		for _, p := range d.Parts {
			if p.Kind == session.PartText {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}
