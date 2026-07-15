package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A cancelled request (request A) must not seed a later, unrelated request (request B):
// when A is interrupted before it answers, the cancel path marks it abandoned, and
// seedPromptIdx must then skip A and seed the turn on B. These tests pin both the pure
// seed selection and the store-level marker write.

func userPromptEvt(t *testing.T, msgID, text string) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: msgID,
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	return event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}, Data: d}
}

func abandonEvt(t *testing.T, msgID string) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.PromptAbandonedData{MsgID: msgID})
	return event.Event{Type: event.TypePromptAbandoned, Actor: event.Actor{Kind: event.ActorSystem, ID: "loop"}, Data: d}
}

func agentPartEvt(t *testing.T) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "ok"}})
	return event.Event{Type: event.TypePartAppended, Actor: event.Actor{Kind: event.ActorAgent, ID: "agent"}, Data: d}
}

func sessionCreatedEvt(t *testing.T) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.SessionCreatedData{Agent: "coder"})
	return event.Event{Type: event.TypeSessionCreated, Actor: event.Actor{Kind: event.ActorSystem, ID: "app"}, Data: d}
}

func TestSeedPromptIdxSkipsAbandoned(t *testing.T) {
	cases := []struct {
		name string
		evs  []event.Event
		want int // index into userPromptEntries order
	}{
		{
			// The reported bug: A cancelled before answering, then unrelated B → seed on B (1).
			"abandoned-A-then-B",
			[]event.Event{userPromptEvt(t, "A", "do A"), abandonEvt(t, "A"), userPromptEvt(t, "B", "do B")},
			1,
		},
		{
			// No abandon marker: A stays the unanswered seed (the pre-fix behavior).
			"unmarked-A-then-B",
			[]event.Event{userPromptEvt(t, "A", "do A"), userPromptEvt(t, "B", "do B")},
			0,
		},
		{
			// A answered normally, then B unanswered → seed on B, unchanged by the new path.
			"answered-A-then-B",
			[]event.Event{userPromptEvt(t, "A", "do A"), agentPartEvt(t), userPromptEvt(t, "B", "do B")},
			1,
		},
		{
			// A follow-up C that augments the cancelled A: A abandoned, so C seeds itself —
			// A's text still lives in the log for context, but it is not the task anchor.
			"abandoned-A-then-followup",
			[]event.Event{userPromptEvt(t, "A", "build X"), abandonEvt(t, "A"), userPromptEvt(t, "C", "also handle errors")},
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := seedPromptIdx(c.evs); got != c.want {
				t.Errorf("seedPromptIdx = %d, want %d", got, c.want)
			}
		})
	}
}

// TestAbandonSeedOnCancelWritesMarker: with A the recorded seed and still unanswered, a
// cancel writes a TypePromptAbandoned marker so a later seed skips A.
func TestAbandonSeedOnCancelWritesMarker(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), nil, Config{})
	ctx := context.Background()
	sid := session.SessionID("s1")
	if _, err := store.Append(ctx, sid, sessionCreatedEvt(t), userPromptEvt(t, "A", "do A")); err != nil {
		t.Fatal(err)
	}
	a.setActiveSeed(sid, "A")

	a.abandonSeedOnCancel(ctx, sid)

	evs, err := store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !abandonedPromptIDs(evs)["A"] {
		t.Fatal("expected A to be marked abandoned after cancel")
	}
	// A later unrelated prompt now seeds on itself, not on the cancelled A.
	if _, err := store.Append(ctx, sid, userPromptEvt(t, "B", "do B")); err != nil {
		t.Fatal(err)
	}
	evs, _ = store.Read(ctx, sid, 0)
	entries := userPromptEntries(evs)
	seed := seedPromptIdx(evs)
	if seed < 0 || seed >= len(entries) || entries[seed].MsgID != "B" {
		t.Fatalf("seed = %d (entries=%d), want the B prompt", seed, len(entries))
	}
}

// TestAbandonSeedOnCancelGuardsAnswered: if the recorded seed already produced an answer
// (stale activeSeedMsgID from a completed turn), a cancel must NOT mark it abandoned.
func TestAbandonSeedOnCancelGuardsAnswered(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), nil, Config{})
	ctx := context.Background()
	sid := session.SessionID("s1")
	if _, err := store.Append(ctx, sid, sessionCreatedEvt(t), userPromptEvt(t, "A", "do A"), agentPartEvt(t)); err != nil {
		t.Fatal(err)
	}
	a.setActiveSeed(sid, "A") // stale: A already answered

	a.abandonSeedOnCancel(ctx, sid)

	evs, _ := store.Read(ctx, sid, 0)
	if abandonedPromptIDs(evs)["A"] {
		t.Error("an already-answered prompt must not be marked abandoned")
	}
}
