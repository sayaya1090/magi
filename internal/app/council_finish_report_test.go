package app

import (
	"context"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"testing"
)

func TestAcceptedExplicitReportDoesNotGenerateAgain(t *testing.T) {
	for _, report := range []string{"수정했고 검증했습니다.", "A longer report is also final regardless of its length."} {
		t.Run(report, func(t *testing.T) {
			fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done}}}
			step := append(textStep(report)[:1], toolStep("council", `{"complete":true,"report_ready":true}`)...)
			llm := workingLLM(step, textStep("DUPLICATE SHOULD NOT APPEAR"))
			a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
			ctx := context.Background()
			sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
			if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "check the file"}}, Actor: event.Actor{Kind: event.ActorUser, ID: "test"}}); err != nil {
				t.Fatal(err)
			}
			evs := waitForTerminal(t, a, sid)
			llm.mu.Lock()
			calls := llm.call
			llm.mu.Unlock()
			if calls != 2 {
				t.Fatalf("model called %d times, want read + report/declaration only", calls)
			}
			finished := 0
			for _, e := range evs {
				if e.Type == event.TypeTurnFinished {
					finished++
				}
			}
			if finished != 1 {
				t.Fatalf("turn.finished=%d", finished)
			}
		})
	}
}

func TestAcceptedDeclarationWithoutReportStillProducesOneAnswer(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done}}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), textStep("final answer"))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "check the file"}}, Actor: event.Actor{Kind: event.ActorUser, ID: "test"}}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, a, sid)
	llm.mu.Lock()
	calls := llm.call
	llm.mu.Unlock()
	if calls != 3 {
		t.Fatalf("want read, declaration, final answer: got %d calls", calls)
	}
}
