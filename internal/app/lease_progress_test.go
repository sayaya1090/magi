package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The lease is reached only when every deterministic test says the child is idle, and a generation
// in flight was not one of them. On a slow model that is where most of a child's wall time goes, so
// the timer landed there, found nothing, asked the judge, and killed a child mid-sentence — four
// subagents in one run whose last recorded event is the provider's own `context canceled`.
//
// But being inside a generation is not by itself work: a wedged backend holds an open stream that
// emits nothing, and extending for that would preserve the exact runaway the cap exists to stop.
// Tokens arriving is the line, and it is the same one the stream watchdog draws.
func TestGeneratingCountsOnlyWhileTokensArrive(t *testing.T) {
	a := &App{}
	const sid = session.SessionID("s_child")

	if a.generating(sid) {
		t.Fatal("a session that has not started a generation must not read as generating")
	}
	a.enterGen(sid)
	if !a.generating(sid) {
		t.Fatal("a generation in flight must read as generating")
	}
	// In flight but silent: nothing has ever arrived, so this is the wedged-backend shape and the
	// lease must NOT treat it as work.
	if a.genFresh(sid) {
		t.Error("a stream that has produced nothing must not read as fresh")
	}
	a.noteGenToken(sid)
	if !a.genFresh(sid) {
		t.Error("a stream that just produced a token must read as fresh")
	}
	a.leaveGen(sid)
	if a.generating(sid) {
		t.Error("the generation ended; nothing is in flight")
	}
	// Freshness is about the stream, not the counter: a token arrived, so it stays fresh — the
	// lease branch requires BOTH, which is what keeps a finished generation from extending.
	if !a.genFresh(sid) {
		t.Error("genFresh reports on the last token, independent of the in-flight count")
	}
}

// The lease's real question is whether the child is producing, and it was measuring elapsed time —
// a proxy that charges a working child and a spinning one identically. bumpProductive is the signal
// it reads: it advances on a real new version or a first-seen exercising command, and NOT on a
// repeat, so one burst of work earns one extension rather than an open lease.
func TestProductiveCountAdvancesPerWorkNotPerTick(t *testing.T) {
	a := &App{}
	const sid = session.SessionID("s_child")

	if n := a.productiveCount(sid); n != 0 {
		t.Fatalf("a session that has produced nothing counts 0, got %d", n)
	}
	a.bumpProductive(sid)
	a.bumpProductive(sid)
	if n := a.productiveCount(sid); n != 2 {
		t.Fatalf("each piece of produced work counts once, got %d", n)
	}
	// The lease compares against the value at the PREVIOUS tick: the same count twice is a window
	// in which nothing was produced, which must not extend.
	last := a.productiveCount(sid)
	if a.productiveCount(sid) > last {
		t.Error("a tick with no new work must not read as progress")
	}
	a.bumpProductive(sid)
	if a.productiveCount(sid) <= last {
		t.Error("work inside the window must read as progress")
	}
}

// A step's retry ladder outlives the dispatch that started it: when the ladder is spent the caller
// re-dispatches the same step, and everything the exhausted attempts established used to be
// dropped at that boundary — the counter restarted at 1 and the trail went back to empty, so the
// next attempt re-walked ground already covered. Ten workers across two steps did exactly that.
func TestStepAttemptLadderContinuesAcrossDispatches(t *testing.T) {
	a := &App{}
	const parent = session.SessionID("s_parent")
	brief := func(task string) port.SpawnRequest {
		return port.SpawnRequest{Agent: "worker",
			Prompt: "curated preamble that differs every time\n\n" + workerPartHeader + task + "\nmore context"}
	}

	// The key is the plan step's own task line, so a re-dispatch whose curated wrapper differs
	// still lands on the same ladder.
	k1 := stepAttemptKey(parent, "worker", brief("design exactly three macros"))
	k2 := stepAttemptKey(parent, "worker", port.SpawnRequest{Agent: "worker",
		Prompt: "a completely different wrapper\n\n" + workerPartHeader + "design exactly three macros\nand other text"})
	if k1 != k2 {
		t.Fatal("the same plan step must key to the same ladder however its brief was curated")
	}
	if k1 == stepAttemptKey(parent, "worker", brief("list every structural difference")) {
		t.Fatal("a different step must key to a different ladder")
	}
	if k1 == stepAttemptKey("s_other", "worker", brief("design exactly three macros")) {
		t.Fatal("another session's step must key to a different ladder")
	}

	if _, n := a.priorStepAttempt(k1); n != 0 {
		t.Fatalf("a step with no history starts at 0, got %d", n)
	}
	a.rememberStepAttempt(k1, port.SpawnResult{Err: "subagent lease expired (judge: KILL)", SessionID: "s_dead"}, 3)
	last, n := a.priorStepAttempt(k1)
	if n != 3 || last.SessionID != "s_dead" {
		t.Fatalf("the continuation must carry the count and the session to read a trail from, got %d %q", n, last.SessionID)
	}
	// A success ends the ladder: the step is done, so the next time it runs it starts clean.
	a.forgetStepAttempt(k1)
	if _, n := a.priorStepAttempt(k1); n != 0 {
		t.Fatalf("a step that got an answer starts clean, got %d", n)
	}
	// A new top-level turn drops every ladder for that session and no other.
	a.rememberStepAttempt(k1, port.SpawnResult{Err: "x"}, 2)
	other := stepAttemptKey("s_other", "worker", brief("design exactly three macros"))
	a.rememberStepAttempt(other, port.SpawnResult{Err: "y"}, 2)
	a.forgetStepAttemptsFor(parent)
	if _, n := a.priorStepAttempt(k1); n != 0 {
		t.Error("a new top-level turn must not inherit the previous task's ladders")
	}
	if _, n := a.priorStepAttempt(other); n != 2 {
		t.Error("another session's ladder must survive")
	}
}
