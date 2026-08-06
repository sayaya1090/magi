package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// scriptLLM returns a scripted sequence of responses, one per StreamChat call — same shape as
// fakeLLM but standalone so these probes don't depend on loop_test ordering.
type scriptLLM struct {
	steps [][]port.ProviderEvent
	call  int
}

func (f *scriptLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 16)
	var evs []port.ProviderEvent
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	} else {
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}}
	}
	f.call++
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func asideText(s string) []port.ProviderEvent {
	return []port.ProviderEvent{{Type: port.ProviderText, Text: s}, {Type: port.ProviderFinish}}
}

func asideCall(name, args string) []port.ProviderEvent {
	return []port.ProviderEvent{
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_" + name, Name: name, Args: json.RawMessage(args)}},
		{Type: port.ProviderFinish},
	}
}

// newAsideApp builds an interactive App with the tools the idle-park handler offers. Production
// registers the human-facing pair in cmd/magi, so mirror that here (they are not in Default()).
func newAsideApp(t *testing.T, llm port.LLMProvider) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.RouteInterjection{})
	reg.Register(builtin.AskUser{})
	a := closeAfter(t, New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: true}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.sessionInfo(context.Background(), sid)
}
