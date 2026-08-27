package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A prompt Steer'd just before the interrupt — in the log but not yet detected into the in-memory
// queue — is still cleared by the cancel. It used to survive (activeSeedMsgID and pendingInterject
// both missed it) and run later as the seed of the user's next, newer request.
func TestCancelClearsAnUndetectedQueuedPrompt(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s_undetected")
	seedSession(t, a, sid)

	// A is the running seed; B was Steer'd into the log a moment before Esc and never detected, so
	// it is NOT in pendingInterject.
	for _, m := range []struct{ id, txt string }{{"mA", "A"}, {"mB", "B"}} {
		pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: m.id, Parts: []session.Part{{Kind: session.PartText, Text: m.txt}}})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, pd); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.activeSeedMsgID = "mA"
	st.pendingInterject = nil // B was never detected
	a.mu.Unlock()

	// 멈춤을 누른 그 순간이 쓸 범위를 정한다(Interrupt가 그때의 미응답 집합을 붙든다) —
	// 정리 중에 도착한 요청까지 쓸지 않으려고. 실행 중인 턴이 없으면 취소할 것도 없고,
	// 붙드는 일만 한다.
	_ = a.Interrupt(ctx, command.Interrupt{SessionID: sid})
	a.abandonSeedOnCancel(ctx, sid)

	evs, _ := a.store.Read(ctx, sid, 0)
	ab := abandonedPromptIDs(evs)
	if !ab["mB"] {
		t.Fatal("the undetected queued prompt mB survived the cancel — it will run ahead of a newer request")
	}
	// A fresh request mD must seed the next turn, not mB.
	pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "mD", Parts: []session.Part{{Kind: session.PartText, Text: "D"}}})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, pd)
	evs, _ = a.store.Read(ctx, sid, 0)
	entries := userPromptEntries(evs)
	if idx := seedPromptIdx(evs, nil); idx < 0 || entries[idx].MsgID != "mD" {
		t.Errorf("next seed should be mD, not the cancelled mB")
	}
}

// Cancelling the running turn (Esc) clears the interjection QUEUE too, not just the
// current seed: a forgotten queued request must not seed the next turn ahead of the
// user's newest request. Each drained item is marked abandoned so seedPromptIdx skips
// it, and a user-facing note reports how many were cleared.
func TestCancelClearsQueue(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s_cancelq")
	seedSession(t, a, sid)

	// Establish the session with three real user prompts: A (seed) + B, C (queued).
	for _, m := range []struct{ id, txt string }{{"mA", "A"}, {"mB", "B"}, {"mC", "C"}} {
		pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: m.id, Parts: []session.Part{{Kind: session.PartText, Text: m.txt}}})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, pd); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.activeSeedMsgID = "mA"
	st.pendingInterject = []pendingInterjection{{MsgID: "mB", Text: "B"}, {MsgID: "mC", Text: "C"}}
	a.mu.Unlock()

	// 멈춤을 누른 그 순간이 쓸 범위를 정한다(Interrupt가 그때의 미응답 집합을 붙든다) —
	// 정리 중에 도착한 요청까지 쓸지 않으려고. 실행 중인 턴이 없으면 취소할 것도 없고,
	// 붙드는 일만 한다.
	_ = a.Interrupt(ctx, command.Interrupt{SessionID: sid})
	a.abandonSeedOnCancel(ctx, sid)

	a.mu.Lock()
	q := a.stateLocked(sid).pendingInterject
	a.mu.Unlock()
	if len(q) != 0 {
		t.Fatalf("queue should be cleared on cancel, got %d item(s)", len(q))
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	ab := abandonedPromptIDs(evs)
	for _, id := range []string{"mA", "mB", "mC"} {
		if !ab[id] {
			t.Errorf("%s should be abandoned after cancel", id)
		}
	}
	// A follow-up request (mD) must now seed the next turn — not the forgotten mB.
	pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "mD", Parts: []session.Part{{Kind: session.PartText, Text: "D"}}})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, pd)
	evs, _ = a.store.Read(ctx, sid, 0)
	entries := userPromptEntries(evs)
	if idx := seedPromptIdx(evs, nil); idx < 0 || entries[idx].MsgID != "mD" {
		got := "?"
		if idx >= 0 && idx < len(entries) {
			got = entries[idx].MsgID
		}
		t.Errorf("next seed should be mD (newest), got %s", got)
	}
}
