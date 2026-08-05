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
// They are NOT triaged one by one. At the finish boundary the whole pending queue is coalesced into
// a single prompt, in arrival order, and ONE disposition is decided for the combined text — so a
// batch holding any work escalates as a unit, questions included. That is the surprising half and
// the reason for this test: a reader of triageQueued alone would expect a decision per message, and
// every other multi-message test hands triageAwareLLM a constant routeAside, so all of them escalate
// together and a mixed batch is never exercised.
//
// What the coalescing has to guarantee is that nothing is lost or reordered: every message's text
// reaches the re-emitted prompt in the order it was typed, the ones merged away are recorded
// resolved and abandoned so a reload does not resurrect them, and the queue ends empty.
func TestAMixedBatchCoalescesInOrderAndLosesNothing(t *testing.T) {
	a, sid := runWithSteers(t,
		func(aside string) bool { return strings.Contains(aside, "WWW") },
		"QQQ what does this repo do", "WWW write the parser", "ZZZ how many files did you touch")

	// One re-emitted prompt carries the batch, and it carries ALL of it, in arrival order.
	res := resurfacedPrompts(t, a, sid)
	if len(res) != 1 {
		t.Fatalf("the batch must re-surface as one prompt, got %d: %q", len(res), res)
	}
	qi, wi, zi := strings.Index(res[0], "QQQ"), strings.Index(res[0], "WWW"), strings.Index(res[0], "ZZZ")
	if qi < 0 || wi < 0 || zi < 0 {
		t.Fatalf("a message was dropped from the coalesced prompt: %q", res[0])
	}
	if !(qi < wi && wi < zi) {
		t.Errorf("the coalesced prompt reordered what the user typed: %q", res[0])
	}

	// Nothing is left holding an unanswered message.
	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("%d message(s) still queued after the run went idle", left)
	}

	// The two merged away are recorded resolved AND abandoned: resolved so a reload does not read
	// them as still waiting, abandoned so seedPromptIdx never starts a turn from one. The carrier
	// needs neither — its re-emission is its own record.
	for _, q := range []string{"QQQ", "WWW"} {
		if !ledgeredResolved(t, a, sid, q) {
			t.Errorf("%s was merged away but never recorded resolved — a reload will re-mask it", q)
		}
		if !markedAbandoned(t, a, sid, q) {
			t.Errorf("%s was merged away but not marked abandoned — it can seed a turn of its own", q)
		}
	}
}

// The disposition is computed, not fixed: a batch with no work in it is answered where it is
// dequeued and re-surfaces nothing. Without this, the test above would pass just as well against a
// loop that escalated everything.
func TestABatchOfOnlyQuestionsIsAnsweredWithoutANewTurn(t *testing.T) {
	a, sid := runWithSteers(t, func(string) bool { return false },
		"QQQ what does this repo do", "ZZZ how many files did you touch")

	if res := resurfacedPrompts(t, a, sid); len(res) != 0 {
		t.Errorf("questions answered at the boundary must not re-run as their own turn: %q", res)
	}
	for _, q := range []string{"QQQ", "ZZZ"} {
		if !ledgeredResolved(t, a, sid, q) {
			t.Errorf("%s was answered inline but never recorded resolved", q)
		}
	}
	a.mu.Lock()
	left := len(a.stateLocked(sid).pendingInterject)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("%d message(s) still queued after the run went idle", left)
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
