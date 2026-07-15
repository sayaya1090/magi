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

// summaryEmptyLLM answers the first turn normally, then returns an empty stream (finish
// with no text) for every later call — i.e. the summarization call yields nothing, the
// case the old stub papered over.
type summaryEmptyLLM struct{ call int }

func (l *summaryEmptyLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	l.call++
	ch := make(chan port.ProviderEvent, 2)
	if l.call == 1 {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func lastCompaction(t *testing.T, a *App, sid session.SessionID) (event.CompactionData, bool) {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypeCompaction {
			var d event.CompactionData
			if err := json.Unmarshal(evs[i].Data, &d); err != nil {
				t.Fatal(err)
			}
			return d, true
		}
	}
	return event.CompactionData{}, false
}

// TestManualCompactUsesRealSummary pins the fix for the /compact wipe bug: manual
// compaction must write the model's actual summary (here fakeLLM's "done"), never the
// old fixed "[compacted N earlier messages]" stub that collapsed the whole context.
func TestManualCompactUsesRealSummary(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "do a thing")

	if err := a.Compact(ctx, command.Compact{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	d, ok := lastCompaction(t, a, sid)
	if !ok {
		t.Fatal("expected a compaction event")
	}
	if strings.HasPrefix(d.Summary, "[compacted ") {
		t.Fatalf("compaction used the stub summary, not a real one: %q", d.Summary)
	}
	if strings.TrimSpace(d.Summary) == "" {
		t.Fatal("compaction summary is empty")
	}
	if d.TokensBefore == 0 {
		t.Fatal("TokensBefore should reflect the pre-compaction context")
	}
}

// TestManualCompactEmptySummaryDoesNotWipe pins the guard: when the summarizer yields
// nothing, Compact must NOT append a compaction that replaces the context — it errors
// out and leaves the conversation intact.
func TestManualCompactEmptySummaryDoesNotWipe(t *testing.T) {
	a, wd := newApp(t, &summaryEmptyLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "do a thing")

	if err := a.Compact(ctx, command.Compact{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err == nil {
		t.Fatal("expected an error when the summary is empty")
	}
	if _, ok := lastCompaction(t, a, sid); ok {
		t.Fatal("no compaction event must be written when the summary is empty")
	}
}
