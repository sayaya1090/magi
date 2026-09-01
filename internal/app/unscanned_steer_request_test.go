package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The request that actually goes out must not carry a steer this step never scanned.
//
// The sibling test measures the filter; this one measures whether anyone uses it, which is the half
// that was wrong before and the half a helper test cannot see. It builds the real request through
// buildStepRequest, with the window genuinely open: a skill has arrived, so the step re-reads the
// log to pick up the note it just appended, and a steer has landed since the scan.
//
// What must come out: the task, and the arrival note. What must not: the message the person typed
// into the middle of the turn — because nothing in that request would mark it as a second, separate
// request, and a model handed two asks at once answers both, or answers one of them twice.
func TestTheOutgoingRequestLeavesAnUnscannedSteerBehind(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	// A skill the session has not seen: this is what makes the step re-read the log at all.
	skills := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "tidy.md"),
		[]byte("---\nname: tidy\ndescription: tidy things\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.stateLocked(sid).skillBlockSet = true // the head is written, so arrivals are appended
	a.mu.Unlock()

	// The task this step is running — scanned, and therefore part of its world.
	if err := a.appendPrompt(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "count the rows in report.csv"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}); err != nil {
		t.Fatal(err)
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}

	// And now the person types, after this step's scan and before its re-read. Nothing queued it:
	// only the top-of-loop scan does that, and it has already run for this step.
	if err := a.appendPrompt(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "STEER-MARKER also check the header"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}); err != nil {
		t.Fatal(err)
	}
	if a.hasPendingInterject(sid) {
		t.Fatal("the premise of this test is that nothing queued it — the window is between the scan and the re-read")
	}

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, agent: AgentSpec{Name: "main"}, depth: 0, maxSteps: 8,
		actor: event.Actor{Kind: event.ActorAgent, ID: "main"}, runStart: time.Now(), guard: newRunGuard(nil)}
	req, _ := a.buildStepRequest(ctx, tc, evs, 1, 0)

	var sent strings.Builder
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			sent.WriteString(p.Text)
			sent.WriteString("\n")
		}
	}
	body := sent.String()

	if !strings.Contains(body, "count the rows in report.csv") {
		t.Error("the task the turn is on must be in its own request")
	}
	if !strings.Contains(body, "tidy") {
		t.Fatal("the skill arrival is what opens this window; it is not in the request, so the test " +
			"is not measuring the thing it says it measures")
	}
	if strings.Contains(body, "STEER-MARKER") {
		t.Error("a steer that landed after this step's scan went out inside this step's request — " +
			"the model is being handed two asks with nothing to tell them apart")
	}
}
