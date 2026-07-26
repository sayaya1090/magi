package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The plan-freeze fix: a routed mid-turn steer regrounds at a COST that depends on the
// route. A "redirect" (the goal changed) rebuilds the plan — reground(true), which re-runs
// the pre-flight planner: fresh decomposition + a new plan audit + explorer re-dispatch. An
// "append" FREEZES the just-approved plan — reground(false), so NO re-plan / NO re-audit /
// NO explorer re-dispatch — and instead injects the steer as a constraint on the in-progress
// work. That difference is the whole bug fix: it stops a mid-turn add-on from reopening a
// unanimously-approved plan audit (bug #1) and from cancelling+re-dispatching the explorers
// that made trivial follow-ups arrive badly delayed and out of order (bug #2).
//
// applyInterjectRoute takes the reground closure as a parameter (like honorReplan), so this
// probe drives it with a stub reground that records the rebuild flag — proving the branch
// without a live model.
func TestPlanFreezeOnAppendSteer(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_test"
	// injectSteerConstraint → appendPromptText needs a session.created first.
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, event.Actor{Kind: event.ActorSystem, ID: "test"}, scd); err != nil {
		t.Fatal(err)
	}

	const orig = "build the parser and verify it"
	const steer = "use only the standard library"

	type ground struct {
		called  bool
		rebuild bool
	}
	var g ground
	reground := func(rebuild bool) { g = ground{called: true, rebuild: rebuild} }

	// --- append: plan FROZEN (reground(false)), steer injected as a keep-plan constraint ---
	a.enqueueInterject(context.Background(), sid, "m_steer", steer)
	nt, changed := a.applyInterjectRoute(ctx, sid, "append", orig, "m_steer", steer, reground)
	if !changed {
		t.Fatal("append should absorb the steer (changed=true)")
	}
	if nt != orig+"\n\n"+steer {
		t.Fatalf("append should fold the steer into turnTask, got %q", nt)
	}
	if !g.called || g.rebuild {
		t.Fatalf("append MUST reground without rebuilding the plan (plan freeze), got called=%v rebuild=%v", g.called, g.rebuild)
	}
	if a.hasPendingInterject(sid) {
		t.Fatal("an absorbed steer must be consumed from the queue")
	}
	txt := sessionText(t, a, sid)
	if !strings.Contains(txt, "Mid-task steer") || !strings.Contains(txt, steer) ||
		!strings.Contains(txt, "do NOT restart, re-plan") {
		t.Fatalf("append should inject a keep-plan steer constraint; session text:\n%s", txt)
	}

	// --- redirect: goal changed → plan REBUILT (reground(true)), no constraint injected ---
	g = ground{}
	const redir = "actually, benchmark it instead"
	beforeLen := countEvents(t, a, sid)
	nt2, changed2 := a.applyInterjectRoute(ctx, sid, "redirect", orig, "m_redir", redir, reground)
	if !changed2 || nt2 != redir {
		t.Fatalf("redirect should re-anchor turnTask to the new goal, got (%q, %v)", nt2, changed2)
	}
	if !g.called || !g.rebuild {
		t.Fatalf("redirect MUST reground with a plan rebuild, got called=%v rebuild=%v", g.called, g.rebuild)
	}
	if countEvents(t, a, sid) != beforeLen {
		t.Fatal("redirect must NOT inject a steer constraint — no plan-freeze path — yet an event was appended")
	}

	// --- queue/"": nothing routed → no reground, task untouched, interjection stays queued ---
	g = ground{}
	nt3, changed3 := a.applyInterjectRoute(ctx, sid, "queue", orig, "m_later", "later thing", reground)
	if changed3 || nt3 != orig {
		t.Fatalf("queue should leave the task untouched, got (%q, %v)", nt3, changed3)
	}
	if g.called {
		t.Fatal("queue must not reground at all")
	}
}
