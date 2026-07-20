package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Cancelling the running turn (Esc) clears the interjection QUEUE too, not just the
// current seed: a forgotten queued request must not seed the next turn ahead of the
// user's newest request. Each drained item is marked abandoned so seedPromptIdx skips
// it, and a user-facing note reports how many were cleared.
func TestCancelClearsQueue(t *testing.T) {
	a := newTestApp(t)
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
	if idx := seedPromptIdx(evs); idx < 0 || entries[idx].MsgID != "mD" {
		got := "?"
		if idx >= 0 && idx < len(entries) {
			got = entries[idx].MsgID
		}
		t.Errorf("next seed should be mD (newest), got %s", got)
	}
}
