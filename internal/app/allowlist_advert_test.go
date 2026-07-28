package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// lastToolResultText returns the content of the last tool result appended to a session.
func lastToolResultText(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolResult || d.Part.ToolResult == nil {
			continue
		}
		var s string
		_ = json.Unmarshal(d.Part.ToolResult.Content, &s)
		out = s
	}
	return out
}

// A refusal has to leave the agent somewhere to go. "tool not permitted" alone reports that
// this call failed and nothing about which call would not, so the only move the model has is
// the same call again — observed as four identical retries ending in a loop-guard kill. The
// permitted set comes from toolSpecs, the same function that built the offer, so the list in
// the refusal cannot drift from the list the model was shown.
func TestAllowlistRefusalNamesWhatTheAgentMayCallInstead(t *testing.T) {
	a, sid, wd := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	s := session.Session{ID: sid, Workdir: wd}
	agent := AgentSpec{Name: "explorer", Tools: []string{"read", "grep", "glob", "list", "findcontext"}}

	if !a.gateAllowlist(context.Background(), s, agent, 1, event.Actor{Kind: event.ActorAgent, ID: "explorer"},
		&session.ToolCall{Name: "recall_memory", CallID: "c1"}, "m1") {
		t.Fatal("a tool outside the allowlist must be blocked")
	}
	got := lastToolResultText(t, a, sid)
	for _, want := range []string{"not permitted", "may call", "read", "grep", "findcontext"} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal is missing %q: %s", want, got)
		}
	}
	// The one thing the list cannot fix: a tool nothing here can replace. Say retrying is futile
	// so the model reports the limit instead of spending its whole budget rediscovering it.
	if !strings.Contains(got, "Retrying recall_memory") {
		t.Errorf("refusal must say the identical retry is refused the same way: %s", got)
	}
	// A tool the agent DOES have is not blocked, and writes no result of its own.
	if a.gateAllowlist(context.Background(), s, agent, 1, event.Actor{Kind: event.ActorAgent, ID: "explorer"},
		&session.ToolCall{Name: "grep", CallID: "c2"}, "m2") {
		t.Error("an allowed tool must pass the gate")
	}
}

// The push pointer for recall_memory is an instruction ("call recall_memory with keywords"),
// and an agent whose allowlist lacks the tool cannot carry it out — it calls, is refused, and
// has no reason to stop, because nothing in its window says the tool is unreachable. This is
// how the read-only spec-mine explorer, which is offered five read tools, ended up calling a
// sixth: the experience store is always configured, so the pointer was appended to every
// agent's context regardless of what that agent could reach.
func TestExperiencePointerOnlyForAgentsThatMayCallRecallMemory(t *testing.T) {
	readOnlyTools := []string{"read", "grep", "glob", "list", "findcontext"}
	exp := &countingExperience{}
	a, sid, wd := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Experience: exp})
	s := session.Session{ID: sid, Workdir: wd}
	raw := []session.Message{{ID: "m1", Role: session.RoleUser,
		Parts: []session.Part{{Kind: session.PartText, Text: "explore the repository"}}}}

	for _, c := range []struct {
		name  string
		tools []string
		want  bool
	}{
		{"read-only explorer", readOnlyTools, false},
		{"unrestricted agent", nil, true},
		{"restricted but holds it", append(append([]string{}, readOnlyTools...), "recall_memory"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			vol := a.volatileContext(context.Background(), s, AgentSpec{Name: "a", Tools: c.tools},
				nil, raw, 1, 30, 0)
			if got := strings.Contains(vol, "recall_memory"); got != c.want {
				t.Errorf("advertises recall_memory=%t, want %t — the prompt must promise only what "+
					"the allowlist permits\n%s", got, c.want, vol)
			}
		})
	}
}
