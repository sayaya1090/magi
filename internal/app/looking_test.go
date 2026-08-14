package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A session opened to answer a question may only look, and nothing else in the workspace has to
// wait for it.
//
// Two halves of one fact, which is why they are one test: the role decides the tools, and the same
// role is what lets WritingRun say the workspace is free. Wire only the first and a question runs
// read-only while everything still queues behind it; wire only the second and handed work starts
// beside a turn that can write, which is the collision the queue exists to prevent.
func TestASessionThatOnlyLooksHasReadOnlyToolsAndBlocksNobody(t *testing.T) {
	a := &App{cfg: Config{}}
	a.cfg = a.cfg.withDefaults()

	spec := a.agentFor(session.Session{ID: "s1", Agent: LookingAgent})
	if len(spec.Tools) == 0 {
		t.Fatal("a session that may only look was given the whole tool set")
	}
	for _, n := range spec.Tools {
		if !readOnlyTools[n] {
			t.Errorf("%q is in the allowlist of a session that may only look", n)
		}
	}
	// …and an ordinary one is untouched: its tools are its agent's, narrowed elsewhere.
	if got := a.agentFor(session.Session{ID: "s2"}); len(got.Tools) != 0 {
		t.Errorf("an ordinary session came back with an allowlist: %v", got.Tools)
	}
}

// The gate that handed-over work asks: is anything running that could touch the workspace?
func TestWritingRunIgnoresATurnThatCannotWrite(t *testing.T) {
	a := &App{cfg: Config{}}
	a.cfg = a.cfg.withDefaults()

	// A session that may only look, with a turn in flight.
	looking := session.SessionID("s_looking")
	a.mu.Lock()
	a.stateLocked(looking).meta = session.Session{ID: looking, Agent: LookingAgent}
	a.stateLocked(looking).cancel = func() {}
	a.mu.Unlock()

	if sid, busy := a.Running(); !busy {
		t.Errorf("Running says nothing is in flight while %q is: it answers about the PROCESS", sid)
	}
	if sid, busy := a.WritingRun(); busy {
		t.Errorf("a turn that cannot write is reported as holding the workspace: %q", sid)
	}

	// And an ordinary one does hold it.
	ordinary := session.SessionID("s_work")
	a.mu.Lock()
	a.stateLocked(ordinary).meta = session.Session{ID: ordinary}
	a.stateLocked(ordinary).cancel = func() {}
	a.mu.Unlock()
	if _, busy := a.WritingRun(); !busy {
		t.Error("a turn that can write is not reported as holding the workspace")
	}
}
