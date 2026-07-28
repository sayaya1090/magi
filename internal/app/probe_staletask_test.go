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

// captureLLM answers every call with a plain finish and records each request.
type captureLLM struct {
	mu   sync.Mutex
	reqs []port.ChatRequest
}

func (f *captureLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "done"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// Probe: after an earlier turn asked for a commit, later turns' LLM requests
// must not re-anchor "commit" anywhere OUTSIDE the conversation history — not
// in the system prompt, and not in the final (volatile-context) user message.
func TestProbeStaleTaskAnchoring(t *testing.T) {
	llm := &captureLLM{}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	turn := func(text string) {
		if err := a.Submit(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: text}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
		}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			evs, _ := a.store.Read(ctx, sid, 0)
			finished := 0
			for _, e := range evs {
				if e.Type == event.TypeTurnFinished {
					finished++
				}
			}
			a.mu.Lock()
			running := a.stateLocked(sid).cancel != nil
			a.mu.Unlock()
			if finished > 0 && !running {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("turn %q never finished", text)
	}

	turn("만들어둔 작업을 커밋해줘")
	turn("이제 README 파일의 구조를 분석해줘")

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.reqs) < 2 {
		t.Fatalf("expected requests from both turns, got %d", len(llm.reqs))
	}
	last := llm.reqs[len(llm.reqs)-1]

	if strings.Contains(last.System, "커밋") {
		t.Errorf("stale task leaked into the SYSTEM prompt:\n%s", snippetAround(last.System, "커밋"))
	}
	// History may legitimately contain the old prompt; the injected volatile tail
	// (the FINAL user message carries step budget / notes / retrieved context)
	// must not re-anchor it.
	if n := len(last.Messages); n > 0 {
		final := partsToText(last.Messages[n-1].Parts)
		if !strings.Contains(final, "README") && strings.Contains(final, "커밋") {
			t.Errorf("stale task re-anchored in the final injected message:\n%s", snippetAround(final, "커밋"))
		}
	}
}

func partsToText(ps []session.Part) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Text + "\n")
	}
	return b.String()
}

func snippetAround(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	lo, hi := i-120, i+120
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}
