package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

type fakeProvider struct {
	chunks []port.ContextChunk
	err    error
	calls  int
	delay  time.Duration
}

func (f *fakeProvider) Provide(ctx context.Context, q port.ContextQuery) ([]port.ContextChunk, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.chunks, f.err
}

func TestGatherContextFormatsChunks(t *testing.T) {
	a := &App{contextProviders: []port.ContextProvider{
		&fakeProvider{chunks: []port.ContextChunk{
			{Source: "docs/api.md", Text: "the API uses bearer tokens"},
			{Source: "", Text: "no-source chunk"},
		}},
	}}
	out := a.gatherContext(context.Background(), port.ContextQuery{Prompt: "how do I auth"})
	if !strings.Contains(out, "docs/api.md") || !strings.Contains(out, "bearer tokens") {
		t.Errorf("expected sourced chunk in output, got: %q", out)
	}
	if !strings.Contains(out, "no-source chunk") {
		t.Errorf("expected source-less chunk text, got: %q", out)
	}
}

func TestGatherContextNoProviders(t *testing.T) {
	a := &App{}
	if out := a.gatherContext(context.Background(), port.ContextQuery{Prompt: "x"}); out != "" {
		t.Errorf("expected empty with no providers, got: %q", out)
	}
}

func TestGatherContextDegradesOnError(t *testing.T) {
	good := &fakeProvider{chunks: []port.ContextChunk{{Source: "s", Text: "kept"}}}
	bad := &fakeProvider{err: errors.New("rag down")}
	a := &App{contextProviders: []port.ContextProvider{bad, good}}
	out := a.gatherContext(context.Background(), port.ContextQuery{Prompt: "x"})
	if !strings.Contains(out, "kept") {
		t.Errorf("a failing provider must not suppress a healthy one: %q", out)
	}
}

func TestGatherContextRespectsBudget(t *testing.T) {
	huge := strings.Repeat("x", contextBudget*2)
	a := &App{contextProviders: []port.ContextProvider{
		&fakeProvider{chunks: []port.ContextChunk{{Source: "big", Text: huge}}},
	}}
	out := a.gatherContext(context.Background(), port.ContextQuery{Prompt: "x"})
	if len(out) > contextBudget {
		t.Errorf("output %d exceeds budget %d", len(out), contextBudget)
	}
}

func TestGatherContextTimeoutDegrades(t *testing.T) {
	slow := &fakeProvider{delay: 30 * time.Second, chunks: []port.ContextChunk{{Text: "late"}}}
	a := &App{contextProviders: []port.ContextProvider{slow}}
	done := make(chan string, 1)
	go func() { done <- a.gatherContext(context.Background(), port.ContextQuery{Prompt: "x"}) }()
	select {
	case out := <-done:
		if strings.Contains(out, "late") {
			t.Errorf("slow provider should have timed out, got: %q", out)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("gatherContext did not honor the per-provider timeout")
	}
}
