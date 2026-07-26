package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Repro for the mid-turn-steer anchor-staleness pathology observed live
// (plexus issue #7–#10, session s_139c0c14afc907b4d3969ba8):
//
//	#2   user prompt A: "01_mcp_server.py 완성해 줘"      (task 1)
//	#3–#38 agent works on task 1
//	#39  user prompt B: "02_mcp_client.py 이것도 완성해 줘" (task 2, steered mid-turn)
//	#56  loop no-progress nudge → "Re-read the original task: …01_mcp_server.py…"  (A, not B)
//	#98  council convened with task = "…01_mcp_server.py…"                          (A, not B)
//
// Root cause: runLoop snapshots turnTask ONCE, at step 0
// (loop.go: `if step == 0 { turnTask = lastUserPromptText(evs) }`), and both
//   - the re-grounding nudge (loop.go: clipSpec(turnTask,…))
//   - the termination council (council_gate.go: `task := turnTask`)
// anchor on that frozen value. A user prompt steered in at step>0 is never
// absorbed, so the guard yanks the agent back to task 1 and the council keeps
// judging task 1 — even though the user's live intent has moved to task 2.
//
// This probe reproduces the freeze deterministically (no live model): it runs the
// exact step-0 capture logic against the event log and asserts the anchor diverges
// from the current user intent after a steer.

func upEvt(text string) event.Event {
	d, _ := json.Marshal(event.PromptSubmittedData{
		Parts: []session.Part{{Kind: session.PartText, Text: text}},
	})
	return event.Event{
		Type:  event.TypePromptSubmitted,
		Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		Data:  d,
	}
}

func agentTextEvt(text string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{
		Role: "assistant",
		Part: session.Part{Kind: session.PartText, Text: text},
	})
	return event.Event{
		Type:  event.TypePartAppended,
		Actor: event.Actor{Kind: event.ActorAgent, ID: "default"},
		Data:  d,
	}
}

func TestSteerAnchorGoesStale(t *testing.T) {
	const taskA = `D:\Workspace\actions\2026-AI-PD\01-mcp\01_mcp_server.py 파일 완성해 줘`
	const taskB = `D:\Workspace\actions\2026-AI-PD\01-mcp\02_mcp_client.py 이것도 완성해 줘`

	// Step 0: the loop reads the log and freezes turnTask. At this point only
	// prompt A exists.
	step0Evs := []event.Event{upEvt(taskA)}
	frozen := lastUserPromptText(step0Evs) // mirrors loop.go step-0 capture

	// Later: the agent has worked, and prompt B is steered in mid-turn. The full
	// log now ends with the user's live intent = B.
	fullEvs := []event.Event{
		upEvt(taskA),
		agentTextEvt("01_mcp_server.py 편집 중…"),
		agentTextEvt("구문 테스트 통과"),
		upEvt(taskB), // #39 steer
	}
	current := lastUserPromptText(fullEvs) // what a fresh re-read would anchor on

	if frozen != taskA {
		t.Fatalf("step-0 snapshot should capture task A, got %q", frozen)
	}
	if current != taskB {
		t.Fatalf("live re-read after steer should reflect task B, got %q", current)
	}

	// The pathology: the anchor the nudge and council use (frozen) no longer
	// matches the user's live intent (current). This is exactly why the loop
	// guard re-injected "Re-read the original task: …01_mcp_server.py…" and the
	// council judged task 1, dragging the agent back off task 2.
	if frozen == current {
		t.Fatalf("expected frozen anchor to diverge from live intent after a steer, but both were %q", frozen)
	}

	// And concretely: the re-grounding nudge (loop.go builds it as
	// "…Re-read the original task:\n" + clipSpec(turnTask, 1500)) would embed A,
	// never mentioning the task B the user actually just asked for.
	nudge := "Re-read the original task:\n" + clipSpec(frozen, 1500)
	if !strings.Contains(nudge, "01_mcp_server.py") {
		t.Fatalf("nudge should embed the frozen task A path, got: %s", nudge)
	}
	if strings.Contains(nudge, "02_mcp_client.py") {
		t.Fatalf("BUG FIXED? nudge unexpectedly mentions task B — anchor was refreshed: %s", nudge)
	}
	t.Logf("REPRO OK — frozen anchor=%q ignores steered intent=%q; nudge re-grounds on task A only", frozen, current)
}
