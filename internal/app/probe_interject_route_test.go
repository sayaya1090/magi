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
)

// applyRoute is the pure anchor-routing primitive the loop drains each step. These
// cases pin the three routings: redirect re-anchors, append folds in, queue/empty
// leaves the task untouched (the safe default that keeps the agent on the current task).
func TestApplyRoute(t *testing.T) {
	const a = "task A"
	const b = "task B"
	cases := []struct {
		action  string
		want    string
		changed bool
	}{
		{"redirect", "task B", true},
		{"append", "task A\n\ntask B", true},
		{"queue", "task A", false},
		{"", "task A", false},
		{"bogus", "task A", false},
	}
	for _, c := range cases {
		got, changed := applyRoute(c.action, a, b)
		if got != c.want || changed != c.changed {
			t.Errorf("applyRoute(%q): got (%q,%v), want (%q,%v)", c.action, got, changed, c.want, c.changed)
		}
	}
}

// The mid-turn-steer pathology (probe_steer_anchor_test.go) is that the frozen anchor
// keeps the nudge/council on task A after the user steered to task B. This is the GREEN
// counterpart: once the orchestrator routes the interjection as "redirect", applyRoute
// re-anchors turnTask on task B, so the very nudge that used to drag the agent back to A
// now re-grounds on B — the live intent.
func TestRedirectRefreshesAnchor(t *testing.T) {
	const taskA = `01_mcp_server.py 파일 완성해 줘`
	const taskB = `02_mcp_client.py 이것도 완성해 줘`

	turnTask, changed := applyRoute("redirect", taskA, taskB)
	if !changed || turnTask != taskB {
		t.Fatalf("redirect should re-anchor to task B, got (%q, changed=%v)", turnTask, changed)
	}
	nudge := "Re-read the original task:\n" + clipSpec(turnTask, 1500)
	if !strings.Contains(nudge, "02_mcp_client.py") {
		t.Fatalf("after redirect the nudge should embed task B, got: %s", nudge)
	}
	if strings.Contains(nudge, "01_mcp_server.py") {
		t.Fatalf("after redirect the nudge should no longer mention task A, got: %s", nudge)
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	})
	return a
}

// The pending-interject queue backs the "queue is the safe default" policy: an
// interjection is parked FIFO, survives until drained as its own turn, and is removed
// exactly when redirect/append absorbs it (consumeInterject) so it isn't run twice.
func TestInterjectQueue(t *testing.T) {
	a := newTestApp(t)
	const sid session.SessionID = "s_test"

	if a.hasPendingInterject(sid) {
		t.Fatal("fresh session should have no queued interjection")
	}
	a.enqueueInterject(sid, " first ")
	a.enqueueInterject(sid, "second")
	if !a.hasPendingInterject(sid) {
		t.Fatal("expected a queued interjection")
	}
	// FIFO, trimmed.
	if got := a.takePendingInterject(sid); got != "first" {
		t.Fatalf("FIFO pop should return the trimmed first entry, got %q", got)
	}
	// consume removes a specific entry (the redirect/append absorb path).
	a.consumeInterject(sid, "second")
	if a.hasPendingInterject(sid) {
		t.Fatal("consume should have removed the only remaining interjection")
	}
	if got := a.takePendingInterject(sid); got != "" {
		t.Fatalf("empty queue should pop \"\", got %q", got)
	}
}

// replan is offered only to a plan-eligible agent (planner on, write-capable, below the
// plan-depth cap) — mirroring the env.Replan nil-gating so a read-only or max-depth agent
// isn't advertised a dead tool. This gate is depth-dynamic, so toolSpecs takes depth.
func TestReplanGatedToPlanEligible(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Replan{})
	a := New(store, completingLLM{}, reg, bus.New(), nil, Config{Permission: "allow", Planner: true, MaxPlanDepth: 2})
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	writer := AgentSpec{Name: "default"}                           // allow-all → produces files
	reader := AgentSpec{Name: "readonly", Tools: []string{"read"}} // cannot write

	// Plan-eligible: write-capable, below the depth cap.
	if !hasSpec(a.toolSpecs(writer, false, 0), "replan") {
		t.Error("a plan-eligible orchestrator should be offered replan")
	}
	if !hasSpec(a.toolSpecs(writer, true, 1), "replan") {
		t.Error("a plan-eligible subagent (depth<cap) should be offered replan")
	}
	// Not eligible: at/over the depth cap, or read-only.
	if hasSpec(a.toolSpecs(writer, true, 2), "replan") {
		t.Error("an agent at the plan-depth cap must NOT be offered replan")
	}
	if hasSpec(a.toolSpecs(reader, false, 0), "replan") {
		t.Error("a read-only agent must NOT be offered replan")
	}
}

// Regression for the run-goroutine post-loop deadlock: that block runs while a.mu is held,
// so it must inspect the pending-interject queue INLINE. The self-locking queue helpers
// (hasPendingInterject/takePendingInterject/drain) would re-lock a.mu and wedge the
// goroutine — silently, since turn.finished is already emitted, so only a POST-loop effect
// exposes it. A queued interjection on a clean turn must re-surface as its own user prompt;
// a deadlocked goroutine never gets there. We poll the store (not a.mu-guarded) so a
// regression fails cleanly instead of also hanging the test on the wedged lock.
func TestQueuedInterjectionResurfacedNoDeadlock(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	// Park an interjection before the turn runs; only the post-loop block re-surfaces it.
	a.enqueueInterject(sid, "follow-up: also handle X")
	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}

	// The interjection re-surfaces as a 2nd user prompt.submitted (the "hi" is the 1st).
	deadline := time.Now().Add(5 * time.Second)
	for {
		evs, _ := store.Read(ctx, sid, 0)
		if n := userPrompts(evs); n >= 2 {
			return // re-surfaced → goroutine did not deadlock
		}
		if time.Now().After(deadline) {
			evs, _ := store.Read(ctx, sid, 0)
			t.Fatalf("queued interjection never re-surfaced (%d user prompts) — post-loop block deadlocked", userPrompts(evs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func userPrompts(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			n++
		}
	}
	return n
}

// A tool's Execute callback can't touch loop-local state, so it records a turnControl
// signal the loop drains next step. signalTurnControl must MERGE (a route and a replan
// can be set independently) and takeTurnControl must clear it.
func TestTurnControlSignalMergeAndDrain(t *testing.T) {
	a := newTestApp(t)
	const sid session.SessionID = "s_test"

	a.signalTurnControl(sid, func(tc *turnControl) { tc.route = "redirect"; tc.reason = "user changed course" })
	a.signalTurnControl(sid, func(tc *turnControl) { tc.replan = true })

	tc := a.takeTurnControl(sid)
	if tc.route != "redirect" || !tc.replan || tc.reason != "user changed course" {
		t.Fatalf("merged signal mismatch: %+v", tc)
	}
	if got := a.takeTurnControl(sid); got.route != "" || got.replan {
		t.Fatalf("take should clear the signal, got %+v", got)
	}
}

// honorReplan enforces the anti-abuse budget so replan can't be used to indefinitely
// reset the stall guard: it honors at most maxReplansPerTurn, refuses a back-to-back
// replan with no work in between, and only calls reground on an honored replan.
func TestHonorReplanBudget(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_test"

	// The store requires a session.created before any other append.
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, event.Actor{Kind: event.ActorSystem, ID: "test"}, scd); err != nil {
		t.Fatal(err)
	}

	count, atCalls := 0, -1
	regrounds := 0
	reground := func(bool) { regrounds++ }

	// guard.callCount() counts every tool call INCLUDING the replan call itself, so a
	// back-to-back replan-only step advances curCalls by exactly 1 over the last snapshot;
	// only that +1 must be refused, and real work (+2 or more) honored.

	// 1st replan at callCount 5: honored (first ever), records the call count.
	a.honorReplan(ctx, sid, "premise broke", &count, &atCalls, 5, reground)
	if count != 1 || atCalls != 5 || regrounds != 1 {
		t.Fatalf("first replan should be honored: count=%d atCalls=%d regrounds=%d", count, atCalls, regrounds)
	}

	// 2nd replan with NO work since — the next step held only the replan call, so curCalls
	// advanced by just 1 (to 6). Must be refused, nothing changes.
	a.honorReplan(ctx, sid, "again", &count, &atCalls, 6, reground)
	if count != 1 || regrounds != 1 {
		t.Fatalf("back-to-back replan without work should be refused: count=%d regrounds=%d", count, regrounds)
	}

	// 2nd replan AFTER real work (curCalls jumped to 9 — bash/edit between): honored, hits cap.
	a.honorReplan(ctx, sid, "real dead end", &count, &atCalls, 9, reground)
	if count != maxReplansPerTurn || atCalls != 9 || regrounds != 2 {
		t.Fatalf("replan after work should be honored to the cap: count=%d atCalls=%d regrounds=%d", count, atCalls, regrounds)
	}

	// 3rd replan (past the cap) even with more work: refused, stall guard left intact.
	a.honorReplan(ctx, sid, "still stuck", &count, &atCalls, 20, reground)
	if count != maxReplansPerTurn || regrounds != 2 {
		t.Fatalf("replan past the cap must be refused: count=%d regrounds=%d", count, regrounds)
	}

	// Every call injects a system note (honored or refused) so the agent always learns
	// the outcome — 4 calls → 4 loop-actor prompts on the log.
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem && e.Actor.ID == "loop" {
			notes++
		}
	}
	if notes != 4 {
		t.Fatalf("expected 4 injected replan notes (2 honored + 2 refused), got %d", notes)
	}
}
