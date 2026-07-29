package app

import (
	"context"
	"strings"
	"testing"
)

// The repeat nudge used to send the agent looking for an error that did not exist. Live shape
// (kv-store-grpc, 2026-07-29): the agent started a server as a background job, magi's own tool
// result told it to `poll with bash_output{id:"bg_1"}`, and it did — thirteen times, every one of
// them SUCCEEDING and handing back `[bg_1 running 6m44s]`. A server does not exit, so the answer
// was never going to change. The nudge that fired said to "inspect WHY the last attempts failed
// (read the error, check paths/state)", which asserts a failure the run never had.
//
// A repeat has two shapes and magi does not know which one it is looking at: retrying something
// that broke, or re-asking something that answered. Naming only the first is magi stating a fact
// about the run's history that it never measured. Naming both costs nothing and is true either way.
func TestTheRepeatNudgeDoesNotInventAFailure(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	s := a.sessionInfo(context.Background(), sid)
	tc := turnCtx{s: s, agent: AgentSpec{Name: "coder"}, guard: newRunGuard()}
	tc.guard.blocked = nudgeThreshold // the same call, counted past the threshold
	if kind := tc.guard.shouldNudge(); kind != "blocked" {
		t.Fatalf("an exact repeat past the threshold is the blocked nudge, got %q", kind)
	}
	tc.guard.nudgedBlocked = false // shouldNudge armed it; fire it for real below
	tc.guard.blocked = nudgeThreshold

	a.injectStuckNudge(context.Background(), tc, "build a KV store server", nil)
	txt := sessionText(t, a, sid)

	if strings.Contains(txt, "WHY the last attempts failed") {
		t.Errorf("the nudge presupposes a failure it never measured:\n%s", txt)
	}
	// Both shapes named, so the agent can tell magi which one it is in — magi cannot.
	for _, want := range []string{"FAILING", "SUCCEEDING"} {
		if !strings.Contains(txt, want) {
			t.Errorf("a repeat can be either shape; %q must be named:\n%s", want, txt)
		}
	}
	// Still the same nudge: it reports the count and refuses nothing.
	for _, want := range []string{"is not refusing it", "it is a repeat"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q — the nudge still only reports:\n%s", want, txt)
		}
	}
}
