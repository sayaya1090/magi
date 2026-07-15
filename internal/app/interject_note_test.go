package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A queued interjection's runtime note is ephemeral: it is never persisted to the
// event log, it rides takeInterjectNotes only while the interjection stays queued,
// and it vanishes the moment the interjection resolves — so a later turn (or a
// reload) can no longer see a stale "queued" notice for an already-handled input.
func TestInterjectNoteEphemeralLifecycle(t *testing.T) {
	a, s := stuckDriverApp(t, `{"steps":[]}`, func(string) string { return "" })
	sid := s.ID
	ctx := context.Background()

	a.enqueueInterject(ctx, sid, "m_x1", "please also fix the docs")
	a.noteInterjection(sid, "current task", "m_x1", "please also fix the docs", false)

	// Not persisted: no PromptSubmitted fact carries the runtime note.
	evs, _ := a.store.Read(ctx, sid, 0)
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil {
			for _, p := range d.Parts {
				if strings.Contains(p.Text, "magi runtime note") {
					t.Fatal("the interjection notice must not be persisted to the event log")
				}
			}
		}
	}

	// While queued: the note is served (with the interjection text and its req id).
	note := a.takeInterjectNotes(sid)
	if !strings.Contains(note, "please also fix the docs") || !strings.Contains(note, "QUEUED") {
		t.Fatalf("live note missing content: %q", note)
	}
	// Serving is idempotent while still queued (not consumed).
	if again := a.takeInterjectNotes(sid); !strings.Contains(again, "please also fix the docs") {
		t.Error("queued note must persist across steps until resolved")
	}

	// Resolve it (a route absorb consumes by id) → the note is pruned immediately.
	a.consumeInterjectByID(ctx, sid, "m_x1")
	if left := a.takeInterjectNotes(sid); left != "" {
		t.Errorf("resolved interjection's note must vanish, got %q", left)
	}
}

// The dispatch-case nudge ("you may answer briefly now") is one-shot: consumed by
// the step that serves it, so it cannot echo into later steps or turns.
func TestAsideNoteOnceConsumed(t *testing.T) {
	a, s := stuckDriverApp(t, `{"steps":[]}`, func(string) string { return "" })
	sid := s.ID

	a.noteInterjection(sid, "current task", "m_y1", "quick question?", true)
	first := a.takeInterjectNotes(sid)
	if !strings.Contains(first, "quick question?") {
		t.Fatalf("dispatch nudge missing: %q", first)
	}
	if second := a.takeInterjectNotes(sid); second != "" {
		t.Errorf("dispatch nudge must be one-shot, got %q", second)
	}
}

// After a queued interjection is resurfaced as its own turn, the model context must
// not carry the instruction twice: liveEvents drops the stranded origin, keeping only
// the resurfaced copy that seeds the turn.
func TestLiveEventsDropsResurfacedOrigins(t *testing.T) {
	a, _ := stuckDriverApp(t, `{"steps":[]}`, func(string) string { return "" })
	ctx := context.Background()
	// A store-registered session (the fabricated s_parent has no jsonl path).
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	origin, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_orig", Parts: []session.Part{{Kind: session.PartText, Text: "DO_THE_THING"}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"}, origin)
	copyD, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_copy", ResurfacedFrom: "m_orig",
		Parts: []session.Part{{Kind: session.PartText, Text: "DO_THE_THING"}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"}, copyD)

	evs, _ := a.store.Read(ctx, sid, 0)
	msgs := reconstruct(a.liveEvents(sid, evs))
	n := 0
	for _, m := range msgs {
		if strings.Contains(joinPartText(m.Parts), "DO_THE_THING") {
			n++
			if m.ID == "m_orig" {
				t.Error("stranded origin must be dropped from the model context")
			}
		}
	}
	if n != 1 {
		t.Errorf("instruction appears %d times in the model context, want exactly 1 (the resurfaced copy)", n)
	}
}
