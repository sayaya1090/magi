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

// F5: a mid-turn interjection is masked from the live turn context by its membership in the
// in-memory pending queue. That queue does NOT survive a process kill, so on the next load a
// still-queued interjection leaked back in as a pending prompt and got mixed into the next
// request. The deferral ledger (TypeInterjectionDeferred) lets a reload reconstruct which
// interjections were queued-but-never-resolved so they stay masked. These tests are
// Ollama-free: they drive the ledger, the reconstruction, and the reload masks directly.

// deferMark builds an InterjectionDeferred ledger event for a msgID.
func deferMark(t *testing.T, msgID string, resolved bool) event.Event {
	t.Helper()
	d, err := json.Marshal(event.InterjectionDeferredData{MessageID: msgID, Resolved: resolved})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypeInterjectionDeferred, Data: d}
}

// promptEv builds a PromptSubmitted event; resurfacedFrom links a drained re-emission.
func promptEv(t *testing.T, msgID, text, resurfacedFrom string) event.Event {
	t.Helper()
	d, err := json.Marshal(event.PromptSubmittedData{
		MessageID:      msgID,
		Parts:          []session.Part{{Kind: session.PartText, Text: text}},
		ResurfacedFrom: resurfacedFrom,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypePromptSubmitted, Data: d}
}

// abandonedDeferrals resolves a deferral EXACTLY when the interjection left the queue: a
// Resolved:true ledger entry (absorbed inline/by route) or a resurfaced re-emission (drained
// to its own turn). Everything queued and not so resolved is abandoned (lost to a kill).
func TestAbandonedDeferrals(t *testing.T) {
	cases := []struct {
		name string
		evs  []event.Event
		want []string
	}{
		{"nil-ledger", nil, nil},
		{
			"queued-unresolved-is-abandoned",
			[]event.Event{deferMark(t, "a", false), deferMark(t, "b", false)},
			[]string{"a", "b"},
		},
		{
			"absorbed-inline-not-abandoned",
			[]event.Event{deferMark(t, "a", false), deferMark(t, "a", true)},
			nil,
		},
		{
			"drained-to-own-turn-not-abandoned",
			[]event.Event{deferMark(t, "a", false), promptEv(t, "m_new", "a again", "a")},
			nil,
		},
		{
			"resolved-only-no-deferred",
			[]event.Event{deferMark(t, "a", true)},
			nil,
		},
		{
			"mixed-only-unresolved-survives",
			[]event.Event{
				deferMark(t, "a", false),                          // abandoned
				deferMark(t, "b", false), deferMark(t, "b", true), // absorbed
				deferMark(t, "c", false), promptEv(t, "m_c2", "c", "c"), // drained
			},
			[]string{"a"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := abandonedDeferrals(tc.evs)
			if len(got) != len(tc.want) {
				t.Fatalf("abandoned=%v, want %v", keys(got), tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Fatalf("abandoned=%v, want %q present", keys(got), w)
				}
			}
		})
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Enqueuing an interjection writes a Resolved:false ledger entry; absorbing it (by id, or by
// text) writes a Resolved:true one. Together the ledger records every queue transition except
// the drain (already recorded by the resurfaced prompt's ResurfacedFrom link).
func TestInterjectLedgerWrites(t *testing.T) {
	ctx := context.Background()
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	a.enqueueInterject(ctx, sid, "m_q", "hold on")
	if r := ledgerFor(t, a, sid, "m_q"); len(r) != 1 || r[0] != false {
		t.Fatalf("after enqueue, ledger for m_q = %v, want [false]", r)
	}

	a.consumeInterjectByID(ctx, sid, "m_q")
	if r := ledgerFor(t, a, sid, "m_q"); len(r) != 2 || r[1] != true {
		t.Fatalf("after consume-by-id, ledger for m_q = %v, want [false true]", r)
	}

	// consume-by-text also resolves.
	a.enqueueInterject(ctx, sid, "m_t", "chitchat")
	a.consumeInterject(ctx, sid, "chitchat")
	if r := ledgerFor(t, a, sid, "m_t"); len(r) != 2 || r[1] != true {
		t.Fatalf("after consume-by-text, ledger for m_t = %v, want [false true]", r)
	}
}

// ledgerFor returns the Resolved flags of every InterjectionDeferred entry for msgID, in order.
func ledgerFor(t *testing.T, a *App, sid session.SessionID, msgID string) []bool {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []bool
	for _, e := range evs {
		if e.Type != event.TypeInterjectionDeferred {
			continue
		}
		var d event.InterjectionDeferredData
		if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
			out = append(out, d.Resolved)
		}
	}
	return out
}

// The whole F5 fix end to end: an on-disk log from a prior (killed) process holds a request,
// two interjections queued-but-unresolved, and one that WAS drained. After hydration the two
// abandoned ones are masked from the live turn context (and from task/council derivation) while
// staying in full history; the request and the drained interjection are NOT masked.
func TestReloadMasksAbandonedInterjection(t *testing.T) {
	ctx := context.Background()
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the prior process's log (no in-memory queue survives a kill).
	seed := []event.Event{
		promptEv(t, "m_req1", "refactor calc.py", ""),
		promptEv(t, "m_f1", "also rename the module", ""), // queued interjection …
		deferMark(t, "m_f1", false),                       // … never resolved (killed)
		promptEv(t, "m_f2", "and add a changelog", ""),
		deferMark(t, "m_f2", false),
		promptEv(t, "m_g", "quick: what's the time?", ""), // a drained one …
		deferMark(t, "m_g", false),
		promptEv(t, "m_gnew", "quick: what's the time?", "m_g"), // … re-emitted as its own turn
	}
	for _, e := range seed {
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}

	a.ensureDeferredHydrated(ctx, sid)

	masked := a.deferredInterjectIDs(sid)
	for _, id := range []string{"m_f1", "m_f2"} {
		if !masked[id] {
			t.Fatalf("abandoned interjection %q must be masked from live context, mask=%v", id, keys(masked))
		}
	}
	for _, id := range []string{"m_req1", "m_g", "m_gnew"} {
		if masked[id] {
			t.Fatalf("%q must NOT be masked (request or drained interjection), mask=%v", id, keys(masked))
		}
	}
	// The wider task/council mask must hide them too.
	if seen := a.interjectSeenIDs(sid); !seen["m_f1"] || !seen["m_f2"] {
		t.Fatalf("abandoned interjections must be masked from turnTask/council too, seen=%v", keys(seen))
	}

	// Live turn context (reconstruct of the filtered stream) must drop the abandoned prompts…
	evs, _ := a.store.Read(ctx, sid, 0)
	live := reconstruct(a.liveEvents(sid, evs))
	if bodyContains(live, "rename the module") || bodyContains(live, "add a changelog") {
		t.Fatalf("abandoned interjection text leaked into live turn context")
	}
	// …while keeping the request and the drained interjection's answer-bound re-emission.
	if !bodyContains(live, "refactor calc.py") {
		t.Fatalf("the real request must remain in live context")
	}
	// Full history is untouched: raw reconstruct still shows the abandoned prompts.
	full := reconstruct(evs)
	if !bodyContains(full, "rename the module") || !bodyContains(full, "add a changelog") {
		t.Fatalf("abandoned interjections must remain visible in full history")
	}
}

func bodyContains(msgs []session.Message, sub string) bool {
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, sub) {
				return true
			}
		}
	}
	return false
}

// Hydration is one-shot: the second call is a no-op even if the ledger changed, so a runtime
// enqueue after load is governed by the live queue, never re-scooped into the abandoned set.
func TestHydrateOnceThenRuntimeQueueIndependent(t *testing.T) {
	ctx := context.Background()
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	// One abandoned interjection on disk from before.
	for _, e := range []event.Event{promptEv(t, "m_old", "old ask", ""), deferMark(t, "m_old", false)} {
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}
	a.ensureDeferredHydrated(ctx, sid)
	if !a.deferredInterjectIDs(sid)["m_old"] {
		t.Fatalf("pre-existing abandoned interjection must be masked after hydration")
	}

	// A fresh runtime enqueue writes a new deferred:false mark. A second hydrate must NOT
	// re-scan it into the abandoned set (it is live-queued, not abandoned); it is masked only
	// via the live queue, and leaves the mask when it drains.
	a.enqueueInterject(ctx, sid, "m_live", "live ask")
	a.ensureDeferredHydrated(ctx, sid) // no-op (already hydrated)
	a.consumeInterjectByID(ctx, sid, "m_live")
	if a.deferredInterjectIDs(sid)["m_live"] {
		t.Fatalf("a drained runtime interjection must not be masked (it left the queue)")
	}
	if !a.deferredInterjectIDs(sid)["m_old"] {
		t.Fatalf("the pre-existing abandoned interjection must stay masked")
	}
}
