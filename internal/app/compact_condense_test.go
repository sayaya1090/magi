package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// condenseLLM answers a fold with one text and a condense with another, so a test can make the
// condense come back as long as what it was given — which is what was measured live (2026-11-12,
// oh-my-openagent): the condense rewrote 91–99% of its input, took 6–7 minutes, and was taken.
type condenseLLM struct {
	mu        sync.Mutex
	fold      string
	condense  string
	condensed int
	system    string
}

func (l *condenseLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	l.mu.Lock()
	txt := l.fold
	if strings.Contains(r.System, "Condense") {
		l.condensed++
		l.system = r.System
		txt = l.condense
	}
	l.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: txt}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func condenseApp(t *testing.T, llm *condenseLLM) (*App, session.SessionID) {
	t.Helper()
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "win", ContextWindow: 4000, Tools: true})
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow", Models: reg, CompactRatio: 0.8})) // brief cap = 4000×0.8×0.1 = 320 tokens
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "win"}})
	return a, sid
}

func foldWith(t *testing.T, a *App, sid session.SessionID, llm *condenseLLM, round, brief string) {
	t.Helper()
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("%s%d", round, i), "t")
	}
	llm.mu.Lock()
	llm.fold = brief
	llm.mu.Unlock()
	if !foldNow(t, a, sid) {
		t.Fatalf("fold %s did not happen", round)
	}
}

// A condense that hands back nearly what it was given is not a shorter brief: it is the same brief,
// paid for twice and minutes late. Keep the original and say so.
func TestACondenseThatDoesNotShrinkIsNotTaken(t *testing.T) {
	llm := &condenseLLM{condense: strings.Repeat("C", 1900)} // ~95% of the 2002-char brief
	a, sid := condenseApp(t, llm)
	foldWith(t, a, sid, llm, "a", strings.Repeat("A", 1000))
	foldWith(t, a, sid, llm, "b", strings.Repeat("B", 1000))
	if llm.condensed != 1 {
		t.Fatalf("expected one condense call over the cap, got %d", llm.condensed)
	}
	d, _ := lastCompaction(t, a, sid)
	if strings.Contains(d.Summary, "CCCC") || !strings.Contains(d.Summary, "AAAA") || !strings.Contains(d.Summary, "BBBB") {
		t.Fatalf("a condense that did not shrink was taken: %d chars, C=%v", len(d.Summary), strings.Contains(d.Summary, "CCCC"))
	}
}

// After a condense that did not shrink, the next fold must not pay for it again on a brief that has
// barely grown — that was one 6–7 minute stop every twenty minutes. It is tried again once the
// brief has grown enough for a different answer to be worth the wait.
func TestAFailedCondenseIsNotRetriedUntilTheBriefGrows(t *testing.T) {
	llm := &condenseLLM{condense: strings.Repeat("C", 1900)}
	a, sid := condenseApp(t, llm)
	foldWith(t, a, sid, llm, "a", strings.Repeat("A", 1000))
	foldWith(t, a, sid, llm, "b", strings.Repeat("B", 1000))
	foldWith(t, a, sid, llm, "c", strings.Repeat("D", 50)) // +2.5%
	if llm.condensed != 1 {
		t.Fatalf("a brief that barely grew was condensed again: %d calls", llm.condensed)
	}
	foldWith(t, a, sid, llm, "d", strings.Repeat("E", 1000)) // +50%
	if llm.condensed != 2 {
		t.Fatalf("a brief that grew by half was not tried again: %d calls", llm.condensed)
	}
}

// The condense is told how small to be. Without a number the model kept everything "verbatim" as
// asked and wrote 91–99% of its input back.
func TestTheCondenseIsToldItsTarget(t *testing.T) {
	llm := &condenseLLM{condense: strings.Repeat("C", 400)}
	a, sid := condenseApp(t, llm)
	foldWith(t, a, sid, llm, "a", strings.Repeat("A", 1000))
	foldWith(t, a, sid, llm, "b", strings.Repeat("B", 1000))
	if !strings.Contains(llm.system, "at most 320 tokens") {
		t.Fatalf("the condense was not given its target: %q", llm.system)
	}
	d, _ := lastCompaction(t, a, sid)
	if !strings.Contains(d.Summary, "CCCC") {
		t.Fatal("a condense that did shrink was not taken")
	}
}

// English is counted as it always was (four bytes a token); Korean, which bytes/4 counted at about
// half its measured tokens, is counted one token a character.
func TestBriefTokensCountsKoreanNearItsMeasuredSize(t *testing.T) {
	if got := briefTokens(strings.Repeat("a", 400)); got != 100 {
		t.Fatalf("400 ASCII bytes: %d tokens, want 100", got)
	}
	ko := strings.Repeat("가", 100)
	if got := briefTokens(ko); got != 100 || len(ko)/4 >= got {
		t.Fatalf("100 Hangul: %d tokens (bytes/4 would say %d)", got, len(ko)/4)
	}
}
