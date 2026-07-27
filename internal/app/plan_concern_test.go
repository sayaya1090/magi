package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// workerConcernEnabled defaults ON; explicit off values disable it.
func TestWorkerConcernFlag(t *testing.T) {
	t.Setenv("MAGI_WORKER_CONCERN", "")
	if !workerConcernEnabled() {
		t.Error("default must be ON")
	}
	for _, off := range []string{"0", "off", "false", "no"} {
		t.Setenv("MAGI_WORKER_CONCERN", off)
		if workerConcernEnabled() {
			t.Errorf("%q must disable", off)
		}
	}
}

// The concern is carried VERBATIM (it is the council's own words about this plan), and it is framed
// so a worker holding one step does not read a plan-wide concern as licence to do the other steps.
func TestConcernBriefCarriesTheConcernScopedToTheWorkersPart(t *testing.T) {
	const concern = "step 2 writes the config but nothing ever reloads it"
	out := concernBrief(concern)
	if !strings.Contains(out, concern) {
		t.Fatalf("the concern must ride verbatim:\n%s", out)
	}
	// It must read as a REVIEW finding that was never settled, not as an approved instruction.
	low := strings.ToLower(out)
	for _, want := range []string{"unresolved", "your part"} {
		if !strings.Contains(low, want) {
			t.Errorf("missing %q — the framing is what keeps a worker in its own step:\n%s", want, out)
		}
	}
	if concernBrief("   ") != "" {
		t.Error("a blank concern must render nothing")
	}
	if concernBrief("") != "" {
		t.Error("no concern must render nothing")
	}
	t.Setenv("MAGI_WORKER_CONCERN", "0")
	if concernBrief(concern) != "" {
		t.Error("the flag must be able to restore the main-session-only baseline")
	}
}

// A long concern must not be clipped to nothing, and must still carry its framing — the clip bound
// applies to the council's text, never to the instructions wrapped around it.
func TestConcernBriefClipsTheConcernNotTheFraming(t *testing.T) {
	out := concernBrief(strings.Repeat("x", 5000))
	if len(out) > 3000 {
		t.Fatalf("an unbounded concern would crowd out the worker's own brief, got %d bytes", len(out))
	}
	if !strings.Contains(strings.ToLower(out), "your part") {
		t.Errorf("the scoping framing was clipped away:\n%s", out)
	}
}

// The storage side of the fix: only the UNRESOLVED (approved=false) advice is carried to workers.
// Approved advice is advisory by construction and already rides in the session it was appended to;
// forwarding it too would bury each worker's own part under plan-wide commentary.
func TestOnlyUnresolvedCouncilAdviceReachesWorkers(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})

	a.injectCouncilAdvice(ctx, sid, "consider naming the helper more clearly", true)
	if got := a.cachedConcern(sid); got != "" {
		t.Fatalf("approved advice must not be carried into worker briefs, got %q", got)
	}

	a.injectCouncilAdvice(ctx, sid, "nothing in the plan ever reloads the config", false)
	if got := a.cachedConcern(sid); !strings.Contains(got, "reloads the config") {
		t.Fatalf("the unresolved concern was not stored for the workers: %q", got)
	}

	// A session that never held a concern reads empty rather than panicking on missing state.
	if got := a.cachedConcern(session.SessionID("no-such-session")); got != "" {
		t.Errorf("unknown session must read empty, got %q", got)
	}
}

// The concern is turn-scoped: the next top-level task must not inherit the previous task's
// unresolved plan concern, or every worker in an unrelated run gets briefed on a dead one.
func TestConcernIsClearedForANewTopLevelTask(t *testing.T) {
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	a.injectCouncilAdvice(ctx, sid, "the plan never exercises the error path", false)
	if a.cachedConcern(sid) == "" {
		t.Fatal("precondition: the concern should be stored")
	}
	a.resetForNewTopLevel(sid)
	if got := a.cachedConcern(sid); got != "" {
		t.Errorf("the previous task's concern leaked into the next one: %q", got)
	}
}
