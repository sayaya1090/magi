package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What the agent reads when the council turns a declaration away. "Address what follows" alone was
// read as "make the objection go away by any means": measured live (2026-09-27), an agent asked
// only to count a file's rows honestly said it could not find the file, was turned away twice, and
// then overwrote the workspace's real invoices.csv with ten invented rows so there was something
// to count. The rejection has to say, before any cap lands, that an honest "could not" is a correct
// outcome and that inventing data to satisfy an objection is not.
func TestARejectionSaysAnHonestFailureIsAnAnswer(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "invoices.csv must be in src"},
		{Round: 1, Decision: council.Done},
	}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), toolStep("council", `{"complete":true}`), textStep("done"))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "count the rows of invoices.csv"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	var rejection string
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "does NOT accept this as finished") {
			rejection = string(e.Data)
		}
	}
	if rejection == "" {
		t.Fatal("the rejection never reached the agent")
	}
	// And how to answer a demand the record refutes: the members have a rule for a CONTEST line, and
	// this is the only place the agent learns the line exists (lost in e4acdd23, restored here).
	for _, want := range []string{"honest account of what could not", "Never invent data", "  CONTEST: ", "drops that one point"} {
		if !strings.Contains(rejection, want) {
			t.Errorf("the rejection does not say %q:\n%s", want, rejection)
		}
	}
}
