package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slowSummaryLLM writes its summary ten characters at a time, and before each piece it records
// what a reader outside the process would be told the companion is doing.
type slowSummaryLLM struct {
	a      *App
	sid    session.SessionID
	mu     sync.Mutex
	seen   []string
	totals []int
}

func (p *slowSummaryLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		for i := 0; i < 3; i++ {
			time.Sleep(40 * time.Millisecond)
			d, _ := p.a.Doing(p.sid)
			p.mu.Lock()
			p.seen = append(p.seen, d)
			if pr, ok := p.a.DoingProgress(p.sid); ok {
				p.totals = append(p.totals, pr.Total)
			}
			p.mu.Unlock()
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "abcdefghij"}
		}
	}()
	return ch, nil
}

// A fold is one or more long model calls between steps, and nothing else is drawn while they run.
// The person asked to be told it started and how far it has got: which chunk of how many, how
// much of the summary has arrived, and how long it has been going. Those are counted, not guessed.
func TestCompactionSaysHowFarTheSummaryHasGot(t *testing.T) {
	defer func(d time.Duration) { compactProgressEvery = d }(compactProgressEvery)
	compactProgressEvery = 5 * time.Millisecond
	llm := &slowSummaryLLM{}
	a, sid, wd := newWorkflowApp(t, llm, nil, Config{Permission: "allow"})
	llm.a, llm.sid = a, sid
	source := strings.Repeat("x", summaryMaterialBytes+10)
	if out := a.summarizeMaterial(context.Background(), a.agentFor(session.Session{ID: sid}), session.Session{ID: sid, Workdir: wd}, "summarize", source); out == "" {
		t.Fatal("summary failed")
	}
	all := strings.Join(llm.seen, "\n")
	for _, want := range []string{"part 1/2", "part 2/2", "10 chars", "20 chars"} {
		if !strings.Contains(all, want) {
			t.Fatalf("progress never said %q; it said:\n%s", want, all)
		}
	}
	// The count rides beside the words, so a screen can draw a bar without parsing them.
	two := false
	for _, n := range llm.totals {
		two = two || n == 2
	}
	if !two {
		t.Fatalf("progress never carried a total of 2 parts: %v", llm.totals)
	}
	if !strings.Contains(all, "s elapsed") {
		t.Fatalf("progress never said how long it has run:\n%s", all)
	}
}

// The note is about the fold; once the fold is over it must not keep saying it is folding.
func TestCompactionProgressIsClearedWhenTheFoldEnds(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "do a thing")
	if err := a.Compact(ctx, command.Compact{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	if d, ok := a.Doing(sid); ok {
		t.Fatalf("still says %q after the fold ended", d)
	}
}
