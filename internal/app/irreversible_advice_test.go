package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The gate asks for ADVICE, not a deliberation, and decides on the prose.
//
// It used to reach the members through Deliberate, which tells them to answer as JSON verdicts
// after a requirements walk over the TURN. Asked a plain yes/no about one command, a reader that
// answered the question failed that parse — costing a second full panel prompt on the retry, and
// reaching the decision only because the retry happened to parse. Measured on
// cobol-modernization, 2026-08-23: 1,154 bytes that walked every condition the question named and
// closed "no, it should not run in this form".
func TestIrreversibleGateAsksForProseAndReadsIt(t *testing.T) {
	fc := &fakeCouncil{advice: "No. The task never mentions /tmp, and there is a form that leaves a way back."}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow, a.cfg.Interactive = false, false
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	s.Workdir = t.TempDir()

	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm -rf /tmp/scratch"}`)}
	if !a.gateIrreversible(ctx, s, event.Actor{Kind: event.ActorSystem, ID: "loop"}, tc, nil, "m1") {
		t.Fatal("a refusal in the prose did not stop the call")
	}
	if len(fc.reqs) != 0 {
		t.Fatalf("the gate ran a full deliberation: %d requests", len(fc.reqs))
	}
	if len(fc.adviseReqs) != 1 {
		t.Fatalf("want exactly one advice ask, got %d", len(fc.adviseReqs))
	}
	// The command must be IN the question — the reader cannot judge scope without it.
	if q := fc.adviseReqs[0].Question; !strings.Contains(q, "rm -rf /tmp/scratch") {
		t.Fatalf("the question does not carry the command: %q", q)
	}
}

// Assent lets it through, and still costs one ask rather than a panel.
func TestIrreversibleGateLetsThroughOnAssent(t *testing.T) {
	fc := &fakeCouncil{advice: "Yes. The task asks for a clean rebuild, and this is the ordinary way."}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow, a.cfg.Interactive = false, false
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	s.Workdir = t.TempDir()

	tc := &session.ToolCall{CallID: "c2", Name: "bash", Args: []byte(`{"command":"rm -rf /tmp/build"}`)}
	if a.gateIrreversible(ctx, s, event.Actor{Kind: event.ActorSystem, ID: "loop"}, tc, nil, "m1") {
		t.Fatal("assent still blocked the call")
	}
	if len(fc.reqs) != 0 {
		t.Fatalf("the gate ran a deliberation on the way through: %d", len(fc.reqs))
	}
}

var _ port.Council = (*fakeCouncil)(nil)
