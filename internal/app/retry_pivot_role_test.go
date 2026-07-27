package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// TestRetryPivotNoteMatchesWhatTheAgentCanDo: the retry note tells a failed attempt to take a
// DIFFERENT route — "change the approach", "a prebuilt artifact", "a workaround flag", "a smaller
// first deliverable". Every one of those is a build move, and a read-only agent has no tool that
// can make one, so for the explorer the note asked for the only thing it could not do. Observed: a
// lease-killed repository explorer, handed this note, re-walked its predecessor's exact search
// order — there was no other route open to it. Its pivot is in what it REPORTS, so the note says
// that instead, and the tool trail changes meaning with it: for an executor it is a path not to
// walk again, for an explorer it is ground already covered whose findings are its to hand over.
func TestRetryPivotNoteMatchesWhatTheAgentCanDo(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	last := port.SpawnResult{Err: "subagent lease expired (judge: KILL)"}

	readOnly := AgentSpec{Name: "specmine", Tools: specMineExploreTools}
	if specCanAct(readOnly) {
		t.Fatal("fixture is not read-only — the spec-mine allowlist must grant no write/edit/bash")
	}
	ro := retryPivotNote(ctx, a, readOnly, last, 1)
	acting := retryPivotNote(ctx, a, AgentSpec{Name: "worker", Tools: []string{"read", "write", "bash"}}, last, 1)

	// Shared: both are told which attempt this is and what killed the last one — a retry that does
	// not know it is a retry re-runs the identical failure.
	for _, out := range []string{ro, acting} {
		for _, want := range []string{"Retry 1", "subagent lease expired"} {
			if !strings.Contains(out, want) {
				t.Fatalf("every retry note must carry %q:\n%s", want, out)
			}
		}
	}
	// The read-only note must not name a move its allowlist refuses.
	for _, unwanted := range []string{"DIFFERENT route", "prebuilt artifact", "workaround flag", "smallest useful part"} {
		if strings.Contains(ro, unwanted) {
			t.Errorf("a read-only agent was told to %q — it has no tool that can:\n%s", unwanted, ro)
		}
	}
	// …and it must still be told where its own pivot lies, including that an absence is a finding.
	for _, want := range []string{"FINDINGS, not changes", "did NOT find", "Report early and report partial"} {
		if !strings.Contains(ro, want) {
			t.Errorf("a read-only agent's retry note should say %q:\n%s", want, ro)
		}
	}
	// An acting agent is unchanged — this is a role split, not a rewrite for everyone.
	for _, want := range []string{"DIFFERENT route", "do NOT restart the same long-running path"} {
		if !strings.Contains(acting, want) {
			t.Errorf("an acting agent's retry note should still say %q:\n%s", want, acting)
		}
	}
	// An unknown agent name resolves to an empty allowlist, which reads as "all tools" — the safe
	// direction, since the alternative silences a real executor's pivot.
	if !specCanAct(a.effectiveSpec("nobody", nil)) {
		t.Error("an unknown agent must not be reported as unable to act")
	}
	// The curator's per-spawn allowlist is what the child actually gets, so it — not the configured
	// spec — decides which note is right.
	a.cfg.Agents = map[string]AgentSpec{"worker": {Name: "worker", Tools: []string{"read", "write", "bash"}}}
	if specCanAct(a.effectiveSpec("worker", []string{"read", "grep"})) {
		t.Error("a curated read-only allowlist must override the agent's configured tools")
	}
}
