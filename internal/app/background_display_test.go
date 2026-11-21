package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A background job's result arrives with its completion fact, not its launch. The display (what the
// web console and the terminal draw from) puts it on the launching call, so the row shows what the
// job printed and opens the child it ran; the model's history keeps the launch result it was given.
func TestSessionStateShowsABackgroundJobsResultOnItsCall(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	a.appendPart(ctx, sid, actor, "m1", session.RoleAssistant, session.Part{ID: "p1", Kind: session.PartToolCall,
		ToolCall: &session.ToolCall{CallID: "job", Name: "dummy_plug", Args: json.RawMessage(`{}`)}})
	a.appendPart(ctx, sid, actor, "m2", session.RoleTool, session.Part{ID: "p2", Kind: session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: "job", Background: true, Content: json.RawMessage(`"Started in the background."`)}})
	detail := "exit 1\nboom\n\n[child s_bgchild01, 7 tool call(s)]"
	data, _ := json.Marshal(map[string]any{"callId": "job", "state": "failed", "exit": 1, "detail": detail})
	a.appendBestEffort(ctx, sid, event.TypeToolBackground, actor, data)

	resultOf := func(msgs []session.Message) *session.ToolResult {
		for _, m := range msgs {
			for _, p := range m.Parts {
				if p.ToolResult != nil && p.ToolResult.CallID == "job" {
					return p.ToolResult
				}
			}
		}
		return nil
	}
	msgs, _, err := a.SessionState(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	r := resultOf(msgs)
	if r == nil {
		t.Fatal("no result for the background call")
	}
	var said string
	_ = json.Unmarshal(r.Content, &said)
	if said != detail {
		t.Fatalf("the call shows its launch, not its result: %s", r.Content)
	}
	if r.Background || !r.IsError {
		t.Fatalf("a failed job must read as finished and failed: background=%v isError=%v", r.Background, r.IsError)
	}
	if kids := ChildrenNamed(said); len(kids) != 1 || kids[0] != "s_bgchild01" {
		t.Fatalf("the call cannot open the child it ran: %v", kids)
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	if h := resultOf(reconstruct(evs)); h == nil || string(h.Content) != `"Started in the background."` {
		t.Fatalf("the model's history changed: %+v", h)
	}
}
