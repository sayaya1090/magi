package app

import (
	"context"
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
	a.enqueueInterject(context.Background(), sid, "m_first", " first ")
	a.enqueueInterject(context.Background(), sid, "m_second", "second")
	if !a.hasPendingInterject(sid) {
		t.Fatal("expected a queued interjection")
	}
	// While queued, both events are masked from the live views (keyed by MsgID).
	if ids := a.deferredInterjectIDs(sid); !ids["m_first"] || !ids["m_second"] {
		t.Fatalf("both queued interjection ids should be masked, got %v", ids)
	}
	// consume removes a specific entry (the redirect absorb path), and unmasks it.
	a.consumeInterjectByID(context.Background(), sid, "m_first")
	if ids := a.deferredInterjectIDs(sid); ids["m_first"] {
		t.Fatalf("a consumed interjection must no longer be masked, got %v", ids)
	}
	a.consumeInterjectByID(context.Background(), sid, "m_second")
	if a.hasPendingInterject(sid) {
		t.Fatal("consume should have removed the remaining interjection")
	}
	if ids := a.deferredInterjectIDs(sid); len(ids) != 0 {
		t.Fatalf("empty queue should mask nothing, got %v", ids)
	}
}

// Regression for the run-goroutine post-loop deadlock: that block runs while a.mu is held,
// so it must inspect the pending-interject queue INLINE. The self-locking queue helpers
// (hasPendingInterject/drain) would re-lock a.mu and wedge the
// goroutine — silently, since turn.finished is already emitted, so only a POST-loop effect
// exposes it. A queued interjection on a clean turn must re-surface as its own user prompt;
// a deadlocked goroutine never gets there. We poll the store (not a.mu-guarded) so a
// regression fails cleanly instead of also hanging the test on the wedged lock.
func TestQueuedInterjectionResurfacedNoDeadlock(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The pre-queued interjection needs work → finish-boundary triage escalates it (routeAside →
	// true), so it re-surfaces as its own turn. The drain now pops under the lock then triages
	// unlocked, so the old self-locking-under-a.mu deadlock this guards can no longer occur.
	a := New(store, &triageAwareLLM{routeAside: func(string) bool { return true }}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	// Park an interjection before the turn runs; only the post-loop block re-surfaces it.
	a.enqueueInterject(context.Background(), sid, "m_followup", "follow-up: also handle X")
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

// userPrompts now lives in steer_finish_race_test.go (a tracked file) so CI, which
// excludes these untracked probes, can compile the tests that share it.

// A tool's Execute callback can't touch loop-local state, so it records a turnControl
// signal the loop drains next step. signalTurnControl must MERGE (a route and a finish
// declaration can be set independently) and takeTurnControl must clear it.
func TestTurnControlSignalMergeAndDrain(t *testing.T) {
	a := newTestApp(t)
	const sid session.SessionID = "s_test"

	a.signalTurnControl(sid, func(tc *turnControl) { tc.route = "redirect"; tc.reason = "user changed course" })
	a.signalTurnControl(sid, func(tc *turnControl) { tc.finish = true })

	tc := a.takeTurnControl(sid)
	if tc.route != "redirect" || !tc.finish || tc.reason != "user changed course" {
		t.Fatalf("merged signal mismatch: %+v", tc)
	}
	if got := a.takeTurnControl(sid); got.route != "" || got.finish {
		t.Fatalf("take should clear the signal, got %+v", got)
	}
}
