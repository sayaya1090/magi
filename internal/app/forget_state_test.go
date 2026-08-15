package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// ForgetSessionState is the meeting-close reap's fix for the per-participant leak: it must drop the
// session's in-memory state entry AND its usage counters. A stray later access recreates an empty
// entry (stateLocked), so the drop is safe; here we assert the entry and the usage are actually gone.
func TestForgetSessionStateFreesTheStateAndUsage(t *testing.T) {
	a, _ := completeApp(t, acFailLLM{t}, config.AutocompleteConfig{}, nil, nil)
	const sid = session.SessionID("participant-1")

	// Give the session an in-memory state entry and a usage record, the way a meeting participant's
	// turns would (stateLocked creates the entry on first access).
	a.mu.Lock()
	a.stateLocked(sid).turnNotes = []string{"in the room"}
	a.mu.Unlock()
	a.usage.record(sid, event.Usage{In: 100, Out: 50})

	a.mu.Lock()
	_, ok := a.stateIf(sid)
	a.mu.Unlock()
	if !ok {
		t.Fatal("precondition failed: no state entry to forget")
	}
	if u := a.UsageFor(sid); u.In != 100 || u.Out != 50 {
		t.Fatalf("precondition failed: usage not recorded: %+v", u)
	}

	a.ForgetSessionState(sid)

	// The state entry is gone (stateIf does NOT recreate — only stateLocked does).
	a.mu.Lock()
	_, ok = a.stateIf(sid)
	a.mu.Unlock()
	if ok {
		t.Error("ForgetSessionState left the session's state entry behind")
	}
	// And so are its usage counters.
	if u := a.UsageFor(sid); u.In != 0 || u.Out != 0 {
		t.Errorf("ForgetSessionState did not clear the session's usage: %+v", u)
	}
}
