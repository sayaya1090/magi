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

// multiWaveLLM dispatches a SECOND subagent only after the FIRST one's result is
// in. It also obeys an "All subagents have reported" nudge by synthesizing
// immediately — so if that (removed) premature nudge ever fires after the first
// wave, this turn would finish with only ONE spawn.
type multiWaveLLM struct{}

func (multiWaveLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	last := lastMsgText(r)
	ch := make(chan port.ProviderEvent, 4)
	emit := func(evs ...port.ProviderEvent) {
		for _, e := range evs {
			ch <- e
		}
		close(ch)
	}
	task := func(agent, prompt string) port.ProviderEvent {
		return port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_" + agent, Name: "task",
			Args: json.RawMessage(`{"agent":"` + agent + `","prompt":"` + prompt + `"}`),
		}}
	}
	switch {
	case strings.Contains(last, "All subagents have reported"):
		// A weak model would obey this and stop — proving prematurity if it fires.
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "PREMATURE_SYNTHESIS"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "tester result"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "final synthesis"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "coder result"):
		// First wave is in → dispatch the SECOND wave mid-task.
		emit(task("tester", "WAVE2"), port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "Dispatched"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "waiting"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "WAVE1"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "wave1 done"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "WAVE2"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "wave2 done"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "staged review"):
		// The ORIGINAL user prompt → dispatch the first wave (coder).
		emit(task("coder", "WAVE1"), port.ProviderEvent{Type: port.ProviderFinish})
	default:
		// Any other context (e.g. a dispatch note arriving in a different order under
		// load) must NOT re-dispatch — re-running a completed task would spawn again
		// and make the spawn count flaky. Just wait.
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "waiting"}, port.ProviderEvent{Type: port.ProviderFinish})
	}
	return ch, nil
}

// Dispatching an additional subagent mid-task is not cut short by a premature
// "all done" — the orchestrator waits through both waves and spawns both.
func TestMultiWaveDispatchNotPrematurelyFinished(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	a := New(store, multiWaveLLM{}, reg, bus.New(), nil, Config{
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
		Parts:     []session.Part{{Kind: session.PartText, Text: "do staged review"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	spawns := 0
	premature := false
	finished := false
	timeout := time.After(15 * time.Second)
	for !finished {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("stream closed before finish")
			}
			switch e.Type {
			case event.TypeAgentSpawned:
				spawns++
			case event.TypePartAppended:
				if strings.Contains(string(e.Data), "PREMATURE_SYNTHESIS") {
					premature = true
				}
			case event.TypeTurnFinished:
				finished = true
			case event.TypeError:
				t.Fatalf("turn errored: %s", string(e.Data))
			}
		case <-timeout:
			t.Fatalf("turn did not finish (spawns=%d)", spawns)
		}
	}
	if premature {
		t.Error("orchestrator finished prematurely after the first wave (premature 'all reported')")
	}
	if spawns != 2 {
		t.Errorf("expected both waves dispatched (spawns=2), got %d", spawns)
	}
}
