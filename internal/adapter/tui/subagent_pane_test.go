package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A spawned child opens a pane, the way a background command does.
//
// The strip, the detail view and the side panel already existed and had exactly one producer. A
// child runs for minutes on a session id this model does not subscribe to, so before this the only
// sign of it was the parent's spinner and one clipped line.
func TestASpawnedChildOpensAPaneOnTheStrip(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 120, 40

	started := time.Now().Add(-30 * time.Second)
	running := []app.SubagentJob{{
		ID: "child-1", Tool: "seele_plan", Task: "plan the refactor",
		Started: started, Running: true,
	}}
	if !m.syncSubagentPanes(running) {
		t.Fatal("a running child did not change the strip")
	}
	if len(m.panes) != 1 {
		t.Fatalf("the child opened %d panes", len(m.panes))
	}
	p := m.panes[0]
	if p.role != "seele_plan" {
		t.Errorf("the pane is labelled %q — a reader cannot tell which subagent this is", p.role)
	}
	if !strings.Contains(p.task, "plan the refactor") {
		t.Errorf("the pane does not say what the child was asked: %q", p.task)
	}
	if p.done {
		t.Error("a running child's pane is already marked done")
	}

	// Polling again while it still runs must not open a second pane, and must not restart the fade.
	m.syncSubagentPanes(running)
	if len(m.panes) != 1 {
		t.Fatalf("polling opened %d panes for one child", len(m.panes))
	}

	// When it ends the pane ends with it — once.
	ended := []app.SubagentJob{{
		ID: "child-1", Tool: "seele_plan", Task: "plan the refactor",
		Started: started, Ended: time.Now(), Running: false, Steps: 7,
	}}
	m.syncSubagentPanes(ended)
	if !m.panes[0].done {
		t.Error("the child finished and its pane is still spinning")
	}
	if m.panes[0].dur <= 0 {
		t.Error("the pane does not say how long the child took")
	}
	first := m.panes[0].doneAt
	m.syncSubagentPanes(ended)
	if !m.panes[0].doneAt.Equal(first) {
		t.Error("polling restarted the fade — the pane would never leave the strip")
	}
}

// The pane shows the child's TRANSCRIPT, not a progress line.
//
// A panel with a detail view and a zoom was being fed the parent's one-line spinner heartbeat —
// "spawn · step 3 · read" — so the prompt the child was handed, what it reasoned, the arguments it
// called a tool with, what came back, and what it finally said were all absent. Not rendered
// roughly: never produced. The pane reads the child's own session now, through the same block
// renderer the main transcript uses.
func TestAChildsPaneShowsWhatItWasToldAndWhatItDid(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 120, 40

	p := &agentPane{job: "c", role: "seele_plan", task: "plan it"}
	p.blocks = rebuildBlocks(childTranscript())
	m.panes = append(m.panes, p)

	body := ansi.Strip(m.paneTail(p, 110, 200))
	for what, want := range map[string]string{
		"the prompt it was handed": "refactor the parser",
		"its reasoning":            "the grammar is ambiguous",
		"the tool it called":       "read",
		"the ARGUMENTS it used":    "parser.go",
		"what it finally said":     "here is the plan",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the pane does not show %s (%q):\n%s", what, want, body)
		}
	}

	// The zoom view shows it too — that is the screen a reader opens to find out what happened.
	m.focusPane, m.zoom, m.zoomPane = 0, true, p
	zoom := ansi.Strip(m.renderZoom(110))
	if !strings.Contains(zoom, "refactor the parser") || !strings.Contains(zoom, "parser.go") {
		t.Errorf("the detail view does not carry the child's transcript:\n%s", zoom)
	}
}

// A pane with no child session reads nothing, and an unchanged session reports no change — the
// strip redraws on every tick and a pane that always claims to have moved would redraw forever.
func TestRefreshingAChildWithNothingNewReportsNoChange(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	if m.refreshChildBlocks(&agentPane{}) {
		t.Error("a pane with no child session claimed to have changed")
	}
	sid, err := m.app.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p := &agentPane{sid: sid}
	m.refreshChildBlocks(p)
	if m.refreshChildBlocks(p) {
		t.Error("an unchanged child session reported a change — the strip would redraw every tick")
	}
}

// And it really READS the session. Reporting "no change" for everything would keep the strip quiet
// and the panes empty, which is the state this whole change was fixing.
func TestRefreshingAChildReadsItsSessionIntoThePane(t *testing.T) {
	mm, store := subagentModelWithStore(t)
	m := &mm
	ctx := context.Background()
	sid, err := m.app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1",
		Parts:     []session.Part{{Kind: session.PartText, Text: "refactor the parser"}},
	})
	if _, err := store.Append(ctx, sid, event.Event{
		SessionID: sid, Type: event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "spawn"}, TS: time.Now(), Data: data,
	}); err != nil {
		t.Fatal(err)
	}

	p := &agentPane{sid: sid}
	if !m.refreshChildBlocks(p) {
		t.Fatal("a child session with a prompt in it produced no change")
	}
	if len(p.blocks) == 0 {
		t.Fatal("the pane took no blocks from the child's session")
	}
	if body := ansi.Strip(m.paneTail(p, 90, 50)); !strings.Contains(body, "refactor the parser") {
		t.Errorf("the prompt the child was handed is not in its pane:\n%s", body)
	}
}

// childTranscript is one child's work: the prompt it was given, a thought, a tool call with
// arguments, its result, and the answer.
func childTranscript() []session.Message {
	return []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{
			{Kind: session.PartText, Text: "refactor the parser"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "the grammar is ambiguous around unary minus"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "t1", Name: "read", Args: json.RawMessage(`{"path":"parser.go"}`)}}}},
		{Role: session.RoleTool, Parts: []session.Part{
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
				CallID: "t1", Content: json.RawMessage(`"package parser"`)}}}},
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartText, Text: "here is the plan"}}},
	}
}
