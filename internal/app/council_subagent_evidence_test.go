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
)

// seedChild registers a child session of parent, created at `created`, with one tool
// call + result, as a dispatched subagent's session would look after it ran.
func seedChild(t *testing.T, a *App, parent, child session.SessionID, agent string, created time.Time, toolOut string) {
	t.Helper()
	a.mu.Lock()
	a.stateLocked(child).meta = session.Session{ID: child, Parent: parent, Escalatable: true, Agent: agent, Created: created}
	a.mu.Unlock()
	cd, _ := json.Marshal(event.SessionCreatedData{Agent: agent})
	_ = a.appendFact(context.Background(), child, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorAgent, ID: agent}, cd)
	// A dispatched child's log begins with the dispatch prompt, then its tool work.
	_ = a.appendPromptText(context.Background(), child, event.Actor{Kind: event.ActorAgent, ID: "orchestrator"}, "do your part")
	act := event.Actor{Kind: event.ActorAgent, ID: agent}
	a.appendPart(context.Background(), child, act, "m_c", session.RoleAssistant, session.Part{
		Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c_1", Name: "bash"},
	})
	a.appendToolResult(context.Background(), child, act, "m_c", "c_1", toolOut, false)
}

// The council's evidence must include the tool output of subagents dispatched THIS turn —
// a delegating orchestrator runs no tools itself, so without this the council would judge
// delegated work blind to what the subagents actually did. A child from a PRIOR turn (created
// before this turn's user prompt) must be excluded.
func TestSubagentTurnEvidenceSurfacedAndScoped(t *testing.T) {
	a, wd := newApp(t, workingLLM(), Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	// A stale child from a hypothetical earlier turn, created BEFORE this turn's prompt.
	past := time.Now().Add(-1 * time.Hour)
	seedChild(t, a, sid, "s_old", "coder", past, "STALE_OUTPUT_from_last_turn")

	// This turn begins with a genuine user prompt.
	if err := a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "u"}, "review the three commits"); err != nil {
		t.Fatal(err)
	}
	turnStart := time.Now()

	// Two subagents dispatched this turn, each producing real tool evidence.
	seedChild(t, a, sid, "s_new1", "reviewer", turnStart.Add(1*time.Millisecond), "FRESH_git_log_output_A")
	seedChild(t, a, sid, "s_new2", "reviewer", turnStart.Add(2*time.Millisecond), "FRESH_git_log_output_B")

	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := a.subagentTurnEvidence(ctx, sid, evs)

	for _, want := range []string{"FRESH_git_log_output_A", "FRESH_git_log_output_B", "subagent reviewer"} {
		if !strings.Contains(got, want) {
			t.Errorf("this-turn subagent evidence must include %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "STALE_OUTPUT_from_last_turn") {
		t.Errorf("a child from a previous turn must not leak into this turn's evidence; got:\n%s", got)
	}
}

// The per-item clip must be generous enough to show a small file/output whole, not a
// fragment — the openssl failure had the buggy line past the old 400-byte cap, so the
// council saw a wrong result but never its cause.
func TestCouncilActionClipShowsWholeSmallFile(t *testing.T) {
	// A ~1KB script whose decisive line sits past byte 400.
	body := strings.Repeat("# header padding line\n", 30) // ~630 bytes of preamble
	body += "common_name = subject.split('CN=')[-1]  # THE_BUG_LINE\n"
	if len(body) <= councilActionCap {
		// Guard the intent: the body must exceed the OLD cap (400) to be a real regression test.
		if len(body) <= 400 {
			t.Fatalf("test body must exceed the old 400-byte cap to be meaningful, got %d", len(body))
		}
	}
	out := clipLine(body, councilActionCap)
	if !strings.Contains(out, "THE_BUG_LINE") {
		t.Errorf("councilActionCap=%d clipped away the decisive line past byte 400; the council would see the symptom but not the cause", councilActionCap)
	}
}
