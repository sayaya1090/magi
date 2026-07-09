package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// countUserPromptText counts genuine user prompts whose text contains marker. A
// mid-turn interjection that is properly queued is re-surfaced as its own turn,
// so it appears TWICE (the original steer + the re-surface); a dropped one
// appears once.
func countUserPromptText(t *testing.T, a *App, sid session.SessionID, marker string) int {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(partsText(d.Parts), marker) {
				n++
			}
		}
	}
	return n
}

// Two messages steered into a single blocked step must BOTH be queued and run as
// their own turns — the earlier one must not be dropped. The mid-turn detector
// advances its handled-count by the full jump, so it has to enqueue every prompt
// in the gap, not just the latest (regression: it once enqueued only the last,
// silently losing every interjection but the newest).
func TestSteerMultipleMidTurnAllQueued(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	bt := &blockingTool{started: started, release: release}
	reg := builtin.Default()
	reg.Register(bt)

	// Both queued steers need real work, so finish-boundary triage escalates each to its own
	// turn (routeAside → true). Triage turns don't advance the positional script below.
	rec := &triageAwareLLM{routeAside: func(string) bool { return true }, steps: [][]port.ProviderEvent{
		// Turn A step 0: call the blocking tool (holds the turn open).
		{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_block", Name: "block", Args: json.RawMessage(`{}`)}}, {Type: port.ProviderFinish}},
		// Turn A step 1: finish without absorbing → B and C stay queued.
		textStep("A done"),
		// B's own turn, then C's own turn.
		textStep("B done"),
		textStep("C done"),
	}}

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, rec, reg, bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "AAA review the whole project"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	<-started // turn A is blocked mid-step

	// Two rapid steers land during the single blocked step.
	for _, txt := range []string{"BBB answer this question", "CCC write this code"} {
		if err := a.Steer(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: txt}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	close(release) // let the turn continue

	// Wait until all follow-up turns drain (run goroutine retires + queue empty).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		idle := a.cancels[sid] == nil && len(a.pendingInterject[sid]) == 0
		a.mu.Unlock()
		if idle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Each steered message must have run as its own turn → appear twice.
	if got := countUserPromptText(t, a, sid, "CCC"); got < 2 {
		t.Fatalf("last steer C should have run as its own turn (appear 2x), got %d", got)
	}
	if got := countUserPromptText(t, a, sid, "BBB"); got < 2 {
		t.Fatalf("earlier steer B was dropped: appears %d time(s), want 2 (queued + re-surfaced)", got)
	}
}
