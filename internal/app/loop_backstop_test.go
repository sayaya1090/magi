package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A top-level turn that spends the runaway step backstop must still END on the record. Falling out
// of the step loop used to write nothing: the run goroutine's teardown covers only cancels and
// errors, so the log kept an open turn that the fleet row, UnfinishedTurnOf and a handoff's asker
// all read as "still working" — forever. The landing is the same honest shape an undeclared turn
// gets: one persisted turn.finished, UNVERIFIED, with the reason naming the backstop.
func TestStepBackstopLandsAnHonestFinish(t *testing.T) {
	// A model that never stops calling tools: every step is a read, past any budget.
	steps := make([][]port.ProviderEvent, 0, 8)
	for i := 0; i < 8; i++ {
		steps = append(steps, toolStep("read", `{"path":"x"}`))
	}
	llm := &fakeLLM{steps: steps}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 3})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "loop forever"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})

	got := waitForTerminal(t, a, sid)
	var fin []event.TurnFinishedData
	for _, e := range got {
		if e.Type != event.TypeTurnFinished || e.Seq == 0 { // persisted only — the teardown's transient echo is display
			continue
		}
		var d event.TurnFinishedData
		if json.Unmarshal(e.Data, &d) == nil {
			fin = append(fin, d)
		}
	}
	if len(fin) != 1 {
		t.Fatalf("want exactly 1 persisted turn.finished, got %d (%v)", len(fin), typesOf(got))
	}
	if !fin[0].Unverified {
		t.Error("a backstop landing must be UNVERIFIED — no council read it")
	}
	if !strings.Contains(fin[0].Reason, "backstop") {
		t.Errorf("the reason should name the backstop, got %q", fin[0].Reason)
	}
	// And the session must no longer read as mid-turn.
	if _, open := a.UnfinishedTurnOf(context.Background(), sid); open {
		t.Error("the turn still reads as unfinished after the backstop landing")
	}
}
