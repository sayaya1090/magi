package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
)

// A user label set BEFORE any session exists (an SSO plugin logging in during
// its startup handler — CreateSession runs after the startup event) is latched
// and applied to sessions created afterwards, so the username shows from the
// first turn instead of being written under the empty session id and lost.
func TestUserLabelLatchedBeforeSession(t *testing.T) {
	a, wd := newApp(t, workingLLM(), Config{})
	a.SetUserLabel("", "sayaya") // startup-time login, no session yet

	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.UserLabel(sid); got != "sayaya" {
		t.Fatalf("latched label must apply to the new session, got %q", got)
	}

	// A second session gets the identity too (login is session-independent)…
	sid2, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.UserLabel(sid2); got != "sayaya" {
		t.Fatalf("identity must ride every new session, got %q", got)
	}

	// …and a direct per-session set still wins for that session.
	a.SetUserLabel(sid2, "other")
	if got := a.UserLabel(sid2); got != "other" {
		t.Fatalf("explicit per-session label must win, got %q", got)
	}
	if got := a.UserLabel(sid); got != "sayaya" {
		t.Fatalf("other sessions keep the latched label, got %q", got)
	}
}
