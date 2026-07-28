package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// newSortableID must order lexicographically by creation time so a request id doubles as a
// sort key (the routing fix leans on "oldest queued == lowest id", and the TUI reordering
// feature will pair a response with its request by this order).
func TestSortableIDMonotonic(t *testing.T) {
	a := newSortableID()
	time.Sleep(2 * time.Millisecond)
	b := newSortableID()
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("sortable id must be 32 hex chars, got %d/%d", len(a), len(b))
	}
	if !(a < b) {
		t.Fatalf("a later id must sort after an earlier one: %q !< %q", a, b)
	}
	// The random tail makes two ids in the same instant differ (no collisions).
	if x, y := newSortableID(), newSortableID(); x == y {
		t.Fatalf("two ids must differ, got %q twice", x)
	}
}

// resolveRouteTarget is the heart of the routing fix: a route binds to a SPECIFIC queued
// request — the one the model named by id (exact or short suffix), else the oldest queued —
// never to lastUserPromptText. That is what stops piled interjections from being cross-applied.
func TestResolveRouteTarget(t *testing.T) {
	a := newTestApp(t)
	const sid session.SessionID = "s_test"
	a.enqueueInterject(context.Background(), sid, "m_00000000aaaa", "docs")
	a.enqueueInterject(context.Background(), sid, "m_11111111bbbb", "refactor")

	// No hint → oldest queued (FIFO == lowest sortable id).
	if mid, txt := a.resolveRouteTarget(sid, ""); txt != "docs" || mid != "m_00000000aaaa" {
		t.Fatalf("no-hint route must pick the oldest queued, got (%q,%q)", mid, txt)
	}
	// Exact id → that request.
	if _, txt := a.resolveRouteTarget(sid, "m_11111111bbbb"); txt != "refactor" {
		t.Fatalf("exact id must select its request, got %q", txt)
	}
	// Short suffix handle (what shortReqID surfaces) → that request.
	if _, txt := a.resolveRouteTarget(sid, shortReqID("m_11111111bbbb")); txt != "refactor" {
		t.Fatalf("suffix handle must select its request, got %q", txt)
	}
	// Unknown id → fall back to oldest (never silently drop the signal).
	if _, txt := a.resolveRouteTarget(sid, "m_deadbeef"); txt != "docs" {
		t.Fatalf("unknown id must fall back to oldest, got %q", txt)
	}
	// Empty queue → nothing to route (the drain treats "" as a no-op).
	a.consumeInterjectByID(context.Background(), sid, "m_00000000aaaa")
	a.consumeInterjectByID(context.Background(), sid, "m_11111111bbbb")
	if mid, txt := a.resolveRouteTarget(sid, ""); mid != "" || txt != "" {
		t.Fatalf("empty queue must resolve to nothing, got (%q,%q)", mid, txt)
	}
}

// The multi-interjection bug: two requests pile up mid-turn; each drain must absorb its OWN
// request exactly once. Before the fix the drain re-read lastUserPromptText, so it re-absorbed
// the same message and cross-applied the route. Now the route resolves + consumes by id, so
// the first append takes "docs" and the next resolve moves on to "refactor" — no duplicate, no
// cross-apply.
func TestApplyInterjectRouteIsIdempotentAcrossPiledInterjections(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_test"
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, event.Actor{Kind: event.ActorSystem, ID: "test"}, scd); err != nil {
		t.Fatal(err)
	}
	const id1, id2 = "m_00000000aaaa", "m_11111111bbbb"
	a.enqueueInterject(context.Background(), sid, id1, "docs")
	a.enqueueInterject(context.Background(), sid, id2, "refactor")

	var regroundCalls int
	reground := func() { regroundCalls++ }

	// Step 1: model routes append with no id → oldest ("docs").
	mid, it := a.resolveRouteTarget(sid, "")
	if it != "docs" {
		t.Fatalf("first drain should target the oldest, got %q", it)
	}
	if _, changed := a.applyInterjectRoute(ctx, sid, "append", "base", mid, it, reground); !changed {
		t.Fatal("append should absorb the interjection")
	}
	// docs is consumed; refactor remains — NOT re-absorbed.
	if mid2, it2 := a.resolveRouteTarget(sid, ""); it2 != "refactor" || mid2 != id2 {
		t.Fatalf("after absorbing docs the next drain must move to refactor, got (%q,%q)", mid2, it2)
	}

	// Step 2: next drain absorbs refactor. Queue then empty → a stray re-drain is a no-op.
	mid2, it2 := a.resolveRouteTarget(sid, "")
	if _, changed := a.applyInterjectRoute(ctx, sid, "append", "base", mid2, it2, reground); !changed {
		t.Fatal("second append should absorb refactor")
	}
	if a.hasPendingInterject(sid) {
		t.Fatal("both interjections should be consumed exactly once")
	}
	if _, it3 := a.resolveRouteTarget(sid, ""); it3 != "" {
		t.Fatalf("an empty queue must yield no target (idempotent drain), got %q", it3)
	}
	if regroundCalls != 2 {
		t.Fatalf("each absorbed interjection should reground once, got %d", regroundCalls)
	}
	// Both steers were injected as keep-plan constraints (append path), no plan rebuild.
	txt := sessionText(t, a, sid)
	if !strings.Contains(txt, "docs") || !strings.Contains(txt, "refactor") {
		t.Fatalf("both steers should be injected as constraints; session text:\n%s", txt)
	}
}
