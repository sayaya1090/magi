package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// resultBody returns the tool result for callID as plain text.
func resultBody(t *testing.T, a *App, sid session.SessionID, callID string) string {
	t.Helper()
	res := toolResultFor(t, a, sid, callID)
	if res == nil {
		t.Fatalf("no result for %s", callID)
	}
	var body string
	if err := json.Unmarshal(res.Content, &body); err != nil {
		t.Fatalf("result content must be a JSON string: %v", err)
	}
	return body
}

// An argument magi does not take is dropped, and the note says so on the same result. What it said
// next was true only when the call worked: "the call ran WITHOUT it, so this result does not
// reflect it" describes a narrower result that exists.
//
// Measured live (extract-elf, 2026-08-01): `write{file_name, content}` came back as
//
//	path is required
//	[ignored arguments] write does not take `file_name` — the call ran WITHOUT it, so this
//	result does not reflect it.
//
// The halves contradict each other, and the second reads as a write that happened. Nothing was
// written. Dropping the argument is usually WHY the call failed — file_name was the misspelling
// that left path missing — so the two have to be said together.
func TestTheIgnoredArgumentNoteSaysWhetherTheCallRan(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	s := a.sessionInfo(context.Background(), sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	g := newRunGuard(nil)

	// The failing shape: the misspelling took the required argument with it.
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0, actor,
		&session.ToolCall{CallID: "bad", Name: "write",
			Args: json.RawMessage(`{"file_name":"/x.txt","content":"hello"}`)}, g, "")
	body := resultBody(t, a, sid, "bad")
	if !strings.Contains(body, "file_name") {
		t.Fatalf("the dropped argument is not named, so nothing here is under test:\n%s", body)
	}
	if strings.Contains(body, "this result does not reflect it") {
		t.Errorf("a FAILED call is described as a narrower result that exists:\n%s", body)
	}
	if !strings.Contains(body, "FAILING without it") {
		t.Errorf("the note does not connect the dropped argument to the failure:\n%s", body)
	}

	// The succeeding shape is unchanged: the result is real, just narrower than what was asked.
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0, actor,
		&session.ToolCall{CallID: "ok", Name: "write",
			Args: json.RawMessage(`{"path":"` + s.Workdir + `/x.txt","content":"hello","mode":"append"}`)}, g, "")
	body = resultBody(t, a, sid, "ok")
	if !strings.Contains(body, "mode") {
		t.Fatalf("the dropped argument is not named, so nothing here is under test:\n%s", body)
	}
	if !strings.Contains(body, "the call ran WITHOUT it") {
		t.Errorf("a call that worked no longer says its result is narrower:\n%s", body)
	}
	if strings.Contains(body, "FAILING") {
		t.Errorf("a call that worked is described as having failed:\n%s", body)
	}
}
