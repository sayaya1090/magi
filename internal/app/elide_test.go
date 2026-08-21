package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// When the window closes, the cheapest thing to give up is a recent, bulky, DIGESTED tool result:
// the assistant narrated what it meant (the knowledge survives), the bytes are re-derivable (the
// file is still on disk), and a replacement near the tail re-bills almost nothing — where the
// summarising fold rewrites the head and re-bills the whole conversation behind it. These pin the
// selection rules and the two views.

func elidePart(t *testing.T, callID, content string, role session.Role) event.Event {
	t.Helper()
	raw, _ := json.Marshal(content)
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "m_" + callID, Role: role,
		Part: session.Part{Kind: session.PartToolResult,
			ToolResult: &session.ToolResult{CallID: callID, Content: raw}},
	})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func elideCall(t *testing.T, callID, name string) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "mc_" + callID, Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: callID, Name: name, Args: json.RawMessage(`{}`)}},
	})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func elideText(t *testing.T, id, text string) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "m_" + id, Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: text},
	})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

// grow appends and re-reads, so every event carries a store-assigned seq like the real log.
func elideApp(t *testing.T) (*App, session.SessionID) {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, bus: bus.New(), states: map[session.SessionID]*sessionState{}}
	return a, "s1"
}

func seed(t *testing.T, a *App, sid session.SessionID, evs ...event.Event) []event.Event {
	t.Helper()
	cd, _ := json.Marshal(event.SessionCreatedData{Agent: "a"})
	if err := a.appendFact(context.Background(), sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorSystem, ID: "app"}, cd); err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if err := a.appendFact(context.Background(), sid, e.Type, event.Actor{Kind: event.ActorAgent, ID: "a"}, e.Data); err != nil {
			t.Fatal(err)
		}
	}
	out, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestElidePicksNewestDigestedAndSparesTheLast(t *testing.T) {
	a, sid := elideApp(t)
	big := strings.Repeat("x", 4000)
	evs := seed(t, a, sid,
		userPromptEvt(t, "m1", "do the thing"),
		elidePart(t, "c1", big, session.RoleTool),
		elideText(t, "t1", "the first read told me the format"), // c1 digested
		elidePart(t, "c2", big, session.RoleTool),
		elideText(t, "t2", "the second read confirmed the layout"), // c2 digested
		elidePart(t, "c3", big, session.RoleTool),                  // newest — exempt whatever its state
	)
	n, _ := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 900) // ~one result's worth

	if n != 1 {
		t.Fatalf("one result should cover a %d-token overage, elided %d", 900, n)
	}
	evs, _ = a.store.Read(context.Background(), sid, 0)
	msgs := reconstruct(evs)
	find := func(callID string) string {
		for _, m := range msgs {
			for _, p := range m.Parts {
				if p.ToolResult != nil && p.ToolResult.CallID == callID {
					return string(p.ToolResult.Content)
				}
			}
		}
		t.Fatalf("result %s missing from the view", callID)
		return ""
	}
	if !strings.Contains(find("c2"), "elided") {
		t.Fatal("the NEWEST digested result (c2) is the cheapest cut and should have been taken")
	}
	if strings.Contains(find("c1"), "elided") {
		t.Fatal("c1 was not needed to cover the overage — eliding it is waste")
	}
	if strings.Contains(find("c3"), "elided") {
		t.Fatal("the single newest result is what the model is about to act on — exempt")
	}
	// The person's view keeps the bytes.
	for _, m := range reconstructWhole(evs) {
		for _, p := range m.Parts {
			if p.ToolResult != nil && p.ToolResult.CallID == "c2" && !strings.Contains(string(p.ToolResult.Content), "xxxx") {
				t.Fatal("the whole view lost the original — the log is the record, the stub is only the model's")
			}
		}
	}
}

func TestElideRefusesUndigestedResults(t *testing.T) {
	a, sid := elideApp(t)
	big := strings.Repeat("y", 4000)
	evs := seed(t, a, sid,
		userPromptEvt(t, "m1", "do the thing"),
		elidePart(t, "c1", big, session.RoleTool), // never narrated
		elidePart(t, "c2", big, session.RoleTool), // never narrated
		elidePart(t, "c3", big, session.RoleTool), // newest — exempt
	)
	n, covered := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 900)
	if n != 0 || covered {
		t.Fatalf("nothing here was narrated — eliding would delete knowledge that exists nowhere "+
			"else, which is the fold's job to preserve: elided %d covered %v", n, covered)
	}
}

func TestElideIsIdempotentAndPrefixStable(t *testing.T) {
	a, sid := elideApp(t)
	big := strings.Repeat("z", 4000)
	evs := seed(t, a, sid,
		userPromptEvt(t, "m1", "work"),
		elidePart(t, "c1", big, session.RoleTool),
		elideText(t, "t1", "noted the contents"),
		elidePart(t, "c2", big, session.RoleTool),
		elideText(t, "t2", "and the follow-up"),
		elidePart(t, "c3", "small", session.RoleTool),
	)
	before := render(reconstruct(evs))
	if n, _ := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 900); n != 1 {
		t.Fatalf("want one elision, got %d", n)
	}
	evs, _ = a.store.Read(context.Background(), sid, 0)
	after := render(reconstruct(evs))
	// Everything BEFORE the elided entry is byte-identical — that is the whole point of cutting
	// near the tail instead of folding the head.
	changed := -1
	for i := range before {
		if i < len(after) && after[i] != before[i] {
			changed = i
			break
		}
	}
	if changed < len(before)-3 {
		t.Fatalf("the cut landed at %d of %d — it should sit at the tail, not rewrite the head", changed, len(before))
	}
	// A later pass takes the NEXT candidate — and once candidates are spent, does nothing:
	// an already-stubbed result is never re-elided, however large the overage.
	if n, _ := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 100); n != 1 {
		t.Fatalf("the remaining digested result should be next, got %d", n)
	}
	evs, _ = a.store.Read(context.Background(), sid, 0)
	if n, _ := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 1000000); n != 0 {
		t.Fatal("every candidate is spent — re-eliding a stub burns an event to save nothing")
	}
}

// The council judges "is this actually done" on FRESH tool evidence, and that evidence must be
// the original bytes even after the model's view elided them: the elision exists to spare the
// prompt cache, not to blind the judge. The council path reads the raw log, never reconstruct —
// this pins that seam so a future refactor cannot quietly route it through the model's view.
func TestCouncilEvidenceSurvivesElision(t *testing.T) {
	a, sid := elideApp(t)
	payload := "the crucial verifier output: 17 tests passed " + strings.Repeat("x", 4000)
	evs := seed(t, a, sid,
		userPromptEvt(t, "m1", "run the tests"),
		elideCall(t, "c1", "bash"),
		elidePart(t, "c1", payload, session.RoleTool),
		elideText(t, "t1", "all seventeen passed, work is done"),
		elideCall(t, "c2", "bash"),
		elidePart(t, "c2", strings.Repeat("y", 4000), session.RoleTool),
	)
	if n, _ := a.elideRecentResults(context.Background(), session.Session{ID: sid},
		event.Actor{Kind: event.ActorSystem, ID: "compact"}, evs, 900); n != 1 {
		t.Fatal("setup: expected one elision")
	}
	evs, _ = a.store.Read(context.Background(), sid, 0)

	// The model's view shows the stub…
	seen := ""
	for _, m := range reconstruct(evs) {
		for _, p := range m.Parts {
			if p.ToolResult != nil && p.ToolResult.CallID == "c1" {
				seen = string(p.ToolResult.Content)
			}
		}
	}
	if !strings.Contains(seen, "elided") {
		t.Fatalf("setup: c1 should be stubbed in the model view, got %q", clipLine(seen, 80))
	}
	// …and the council still reads the original.
	ev := turnToolEvidence(evs, 8)
	if !strings.Contains(ev, "17 tests passed") {
		t.Fatalf("the council's evidence lost the elided result's content:\n%s", ev)
	}
	if strings.Contains(ev, "elided to keep the conversation") {
		t.Fatalf("the council is reading the model's stub instead of the log:\n%s", ev)
	}
}
