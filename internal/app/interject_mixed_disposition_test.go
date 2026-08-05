package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Several messages land during one blocked step and they do not all want the same thing: two are
// questions answerable from what the agent already knows, one asks for real work.
//
// Each is decided on its OWN. A question is answered where it is dequeued and never runs again; the
// work items are the only ones that merge, with each other, because they arrived together and are
// one body of work. The batch used to get a single disposition — so one piece of work in it dragged
// every question along into a work prompt, unanswered — and every other multi-message test hands
// triageAwareLLM a constant routeAside, which is why nothing caught it.
//
// The pairing the UI needs comes out of the same split: an inline answer carries InReplyTo naming
// the question it answers, so the two render adjacent, and the work re-surfaces with
// ResurfacedFrom so its bubble lands above the turn that does it.
func TestEachQueuedMessageGetsItsOwnDisposition(t *testing.T) {
	a, sid := runWithSteers(t,
		func(aside string) bool { return strings.Contains(aside, "WWW") },
		"QQQ what does this repo do", "WWW write the parser", "ZZZ how many files did you touch")

	// Only the work re-surfaces, and only it.
	res := resurfacedPrompts(t, a, sid)
	if len(res) != 1 {
		t.Fatalf("exactly the work should run as its own turn, got %d re-surfaced: %q", len(res), res)
	}
	if !strings.Contains(res[0], "WWW") {
		t.Errorf("the re-surfaced prompt is not the work item: %q", res[0])
	}
	for _, q := range []string{"QQQ", "ZZZ"} {
		if strings.Contains(res[0], q) {
			t.Errorf("%s was answered inline, so it must not be dragged into the work prompt: %q", q, res[0])
		}
	}

	// Every question got an answer of its own, tagged with the question it answers — that tag is
	// what puts the two next to each other on screen.
	for _, q := range []string{"QQQ", "ZZZ"} {
		if !answeredInReplyTo(t, a, sid, q) {
			t.Errorf("%s was not answered inline with an InReplyTo tag naming it", q)
		}
	}

	// Nothing is left holding an unanswered message, and every message is accounted for.
	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("%d message(s) still queued after the run went idle", left)
	}
	// The answered ones are ledgered resolved. The work item needs no entry: its re-emission IS
	// its resolution, which is what abandonedDeferrals reads a ResurfacedFrom link as.
	for _, q := range []string{"QQQ", "ZZZ"} {
		if !ledgeredResolved(t, a, sid, q) {
			t.Errorf("%s never reached the deferral ledger as resolved — a reload will re-mask it", q)
		}
	}
}

// Work items merge with each other, in arrival order, and run as ONE turn. Two pieces of work typed
// back to back are one job; making a separate turn of each would re-plan and re-judge twice over
// what the user said in one breath.
func TestWorkItemsMergeWithEachOtherInOrder(t *testing.T) {
	a, sid := runWithSteers(t, func(string) bool { return true },
		"WWW1 write the parser", "QQQ2 and wire it up", "WWW3 then add the tests")

	res := resurfacedPrompts(t, a, sid)
	if len(res) != 1 {
		t.Fatalf("three work items must run as one turn, got %d: %q", len(res), res)
	}
	i1, i2, i3 := strings.Index(res[0], "WWW1"), strings.Index(res[0], "QQQ2"), strings.Index(res[0], "WWW3")
	if i1 < 0 || i2 < 0 || i3 < 0 {
		t.Fatalf("a work item was dropped from the merged prompt: %q", res[0])
	}
	if !(i1 < i2 && i2 < i3) {
		t.Errorf("the merged prompt reordered what the user typed: %q", res[0])
	}
	// The two merged away are recorded resolved AND abandoned: resolved so a reload does not read
	// them as still waiting, abandoned so seedPromptIdx never starts a turn from one.
	for _, q := range []string{"WWW1", "QQQ2"} {
		if !ledgeredResolved(t, a, sid, q) {
			t.Errorf("%s was merged away but never recorded resolved", q)
		}
		if !markedAbandoned(t, a, sid, q) {
			t.Errorf("%s was merged away but not marked abandoned — it can seed a turn of its own", q)
		}
	}
}

// The same sentence typed twice is one request. This is the one part of the old whole-batch merge
// that survives the split: exact repeats collapse, near-duplicates do not.
func TestAnExactRepeatIsAnsweredOnce(t *testing.T) {
	a, sid := runWithSteers(t, func(string) bool { return false },
		"QQQ what does this repo do", "QQQ what does this repo do")

	if n := countUserPromptText(t, a, sid, "QQQ"); n != 2 {
		t.Fatalf("both prompts should still be recorded, got %d", n)
	}
	// One of the two is dropped as a repeat: abandoned, and never separately answered.
	dropped := 0
	for _, e := range readEvents(t, a, sid) {
		if e.Type == event.TypePromptAbandoned {
			dropped++
		}
	}
	if dropped != 1 {
		t.Errorf("exactly one of the two identical messages should be dropped as a repeat, got %d", dropped)
	}
}

// runWithSteers holds a turn open on the blocking tool, lands every steer inside that one step, then
// lets the turn finish and waits for the queue to drain. routeAside decides, per triaged text,
// whether the boundary treats it as work.
func runWithSteers(t *testing.T, routeAside func(string) bool, steers ...string) (*App, session.SessionID) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	reg := builtin.Default()
	reg.Register(&blockingTool{started: started, release: release})

	llm := &triageAwareLLM{routeAside: routeAside, steps: [][]port.ProviderEvent{
		// Turn A step 0: hold the turn open. Step 1: finish, absorbing nothing.
		{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_block", Name: "block", Args: json.RawMessage(`{}`)}}, {Type: port.ProviderFinish}},
		textStep("A done"),
		textStep("the escalated turn's answer"),
	}}

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "AAA review the whole project"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	for _, txt := range steers {
		if err := a.Steer(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: txt}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	close(release)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		st, _ := a.stateIf(sid)
		idle := st == nil || (st.cancel == nil && len(st.pendingInterject) == 0)
		a.mu.Unlock()
		if idle {
			return a, sid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the queue never drained")
	return nil, ""
}

// resurfacedPrompts returns the text of every prompt re-emitted from the queue (ResurfacedFrom set).
func resurfacedPrompts(t *testing.T, a *App, sid session.SessionID) []string {
	t.Helper()
	var out []string
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom != "" {
			out = append(out, partsText(d.Parts))
		}
	}
	return out
}

// msgIDsCarrying returns the ids of the ORIGINAL user prompts whose text contains marker.
func msgIDsCarrying(t *testing.T, a *App, sid session.SessionID, marker string) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom == "" && strings.Contains(partsText(d.Parts), marker) {
			ids[d.MessageID] = true
		}
	}
	return ids
}

// answeredInReplyTo reports whether some assistant text part was persisted carrying InReplyTo that
// names the message containing marker — the tag the TUI reads to sit the answer next to its question.
func answeredInReplyTo(t *testing.T, a *App, sid session.SessionID, marker string) bool {
	t.Helper()
	ids := msgIDsCarrying(t, a, sid, marker)
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.InReplyTo == "" {
			continue
		}
		if ids[d.InReplyTo] && d.Part.Kind == session.PartText && strings.TrimSpace(d.Part.Text) != "" {
			return true
		}
	}
	return false
}

func ledgeredResolved(t *testing.T, a *App, sid session.SessionID, marker string) bool {
	t.Helper()
	ids := msgIDsCarrying(t, a, sid, marker)
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypeInterjectionDeferred {
			continue
		}
		var d event.InterjectionDeferredData
		if json.Unmarshal(e.Data, &d) == nil && ids[d.MessageID] && d.Resolved {
			return true
		}
	}
	return false
}

func markedAbandoned(t *testing.T, a *App, sid session.SessionID, marker string) bool {
	t.Helper()
	ids := msgIDsCarrying(t, a, sid, marker)
	for _, e := range readEvents(t, a, sid) {
		if e.Type != event.TypePromptAbandoned {
			continue
		}
		var d event.PromptAbandonedData
		if json.Unmarshal(e.Data, &d) == nil && ids[d.MsgID] {
			return true
		}
	}
	return false
}

func readEvents(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}
