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

// cancelFlowLLM drives: orchestrator dispatches two subagents (one quick, one
// that blocks until cancelled), goes idle, then synthesizes once results are in;
// a later "second request" is handled directly.
type cancelFlowLLM struct{}

func (cancelFlowLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	last := lastMsgText(r)
	ch := make(chan port.ProviderEvent, 4)
	emit := func(evs ...port.ProviderEvent) {
		for _, e := range evs {
			ch <- e
		}
		close(ch)
	}
	switch {
	case strings.Contains(last, "SECOND_REQUEST"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "handled the new request"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "[subagent"):
		// At least one result is in → synthesize and finish.
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "synthesis of available results"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "Dispatched"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "dispatched; waiting"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "QUICK_TASK"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "quick subagent done"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "BLOCK_TASK"):
		// Subagent that hangs until its context is cancelled (interrupted).
		go func() {
			defer close(ch)
			<-ctx.Done()
		}()
	default:
		// Initial orchestrator turn: dispatch both subagents in one call.
		emit(port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_task", Name: "task",
			Args: json.RawMessage(`{"tasks":[{"agent":"coder","prompt":"QUICK_TASK"},{"agent":"tester","prompt":"BLOCK_TASK"}]}`),
		}}, port.ProviderEvent{Type: port.ProviderFinish})
	}
	return ch, nil
}

// Cancelling one of several in-flight subagents still lets the orchestrator
// transition working→idle (the turn finishes), and a NEW request afterwards runs
// normally.
func TestCancelSubagentThenIdleThenNewRequest(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	a := New(store, cancelFlowLLM{}, reg, bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"coder": {Name: "coder"}, "tester": {Name: "tester"}},
		SubagentStall:       30 * time.Second,
		SubagentTimeout:     30 * time.Second,
		SubagentMaxRestarts: 0,
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "review with subagents"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	// Find the blocking subagent (tester) by its spawn event, then cancel it.
	var blockingChild session.SessionID
	finished := false
	timeout := time.After(10 * time.Second)
	for !finished {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("stream closed before turn finished")
			}
			if e.Type == event.TypeAgentSpawned {
				var d event.AgentStatusData
				if json.Unmarshal(e.Data, &d) == nil && d.Role == "tester" {
					blockingChild = session.SessionID(d.AgentID)
					// Cancel this specific subagent once it's running.
					_ = a.Interrupt(ctx, command.Interrupt{SessionID: blockingChild})
				}
			}
			if e.Type == event.TypeTurnFinished {
				finished = true
			}
			if e.Type == event.TypeError {
				t.Fatalf("turn errored: %s", string(e.Data))
			}
		case <-timeout:
			t.Fatalf("turn did not reach idle after cancelling a subagent (blockingChild=%q)", blockingChild)
		}
	}
	if blockingChild == "" {
		t.Fatal("never saw the tester subagent spawn")
	}

	// Idle reached. A NEW request must run to completion normally.
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "SECOND_REQUEST please"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	sub2, cancelSub2, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub2()
	got2 := false
	timeout2 := time.After(10 * time.Second)
	for !got2 {
		select {
		case e, ok := <-sub2:
			if !ok {
				t.Fatal("stream closed before second turn finished")
			}
			if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "handled the new request") {
				got2 = true
			}
		case <-timeout2:
			t.Fatal("second request did not complete after returning to idle")
		}
	}
}
