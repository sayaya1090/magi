package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A finished tool call reaches an observer that asks for it: which tool, with what arguments, and
// whether it succeeded. The core attaches no meaning to the tool; an extension decides what matters.
type toolObserver struct {
	recordingObserver
	mu    sync.Mutex
	tools []string
}

func (o *toolObserver) ToolFinished(sid, tool, args string, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tools = append(o.tools, fmt.Sprintf("%s|%s|%s|%v", sid, tool, args, ok))
}

func (o *toolObserver) seen() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.tools...)
}

func TestObserverHearsAFinishedTool(t *testing.T) {
	obs := &toolObserver{}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("read", `{"path":"a.txt"}`), asideText("done")}}
	a, wd := newApp(t, llm, Config{Observer: obs, Permission: "allow"})
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "read a.txt"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(obs.seen()) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	got := obs.seen()
	if len(got) != 1 {
		t.Fatalf("ToolFinished calls = %v, want one", got)
	}
	if !strings.HasPrefix(got[0], string(sid)+"|read|") || !strings.Contains(got[0], "a.txt") || !strings.HasSuffix(got[0], "|true") {
		t.Errorf("ToolFinished = %q", got[0])
	}
}

type dispatchObserver struct {
	recordingObserver
	completed chan ToolObservation
}

func (o *dispatchObserver) ToolFinishedWithDispatch(_, _, _ string, _ bool, dispatch ToolObservation) {
	o.completed <- dispatch
}

func TestToolObserverDistinguishesParallelAndSequentialDispatch(t *testing.T) {
	obs := &dispatchObserver{completed: make(chan ToolObservation, 3)}
	first := asideCall("read", `{"path":"a.txt"}`)[0]
	second := asideCall("read", `{"path":"a.txt"}`)[0]
	second.ToolCall.CallID = "second"
	solo := asideCall("read", `{"path":"a.txt"}`)
	solo[0].ToolCall.CallID = "solo"
	llm := &scriptLLM{steps: [][]port.ProviderEvent{{first, second, {Type: port.ProviderFinish}}, solo, asideText("done")}}
	a, wd := newApp(t, llm, Config{Observer: obs, Permission: "allow"})
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "read a.txt"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]ToolObservation)
	for range 3 {
		select {
		case dispatch := <-obs.completed:
			got[dispatch.CallID] = dispatch
		case <-ctx.Done():
			t.Fatalf("waiting for completions: %v", got)
		}
	}
	batch := got["c_read"].ParallelBatch
	if batch == "" || got["second"].ParallelBatch != batch || got["c_read"].OrderKnown || got["second"].OrderKnown ||
		got["c_read"].ParallelSize != 2 || got["second"].ParallelSize != 2 {
		t.Fatalf("parallel dispatch: %v", got)
	}
	if !got["solo"].OrderKnown || got["solo"].ParallelBatch != "" {
		t.Fatalf("sequential dispatch: %v", got)
	}
}

// Calls written in one model response share a response identity even when they run one after another,
// so an observer can count the model steps a stretch of work took (a parallel batch id cannot: a
// sequential multi-call response has none).
func TestToolObserverNamesTheResponseThatWroteEachCall(t *testing.T) {
	obs := &dispatchObserver{completed: make(chan ToolObservation, 3)}
	first := asideCall("read", `{"path":"a.txt"}`)[0]
	second := asideCall("write", `{"path":"b.txt","content":"x"}`)[0]
	second.ToolCall.CallID = "second"
	solo := asideCall("read", `{"path":"a.txt"}`)
	solo[0].ToolCall.CallID = "solo"
	llm := &scriptLLM{steps: [][]port.ProviderEvent{{first, second, {Type: port.ProviderFinish}}, solo, asideText("done")}}
	a, wd := newApp(t, llm, Config{Observer: obs, Permission: "allow"})
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "read and write"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]ToolObservation)
	for range 3 {
		select {
		case dispatch := <-obs.completed:
			got[dispatch.CallID] = dispatch
		case <-ctx.Done():
			t.Fatalf("waiting for completions: %v", got)
		}
	}
	if got["second"].ParallelBatch != "" {
		t.Fatalf("read+write should run one after another: %v", got)
	}
	one := got["c_read"].ResponseID
	if one == "" || got["second"].ResponseID != one || got["solo"].ResponseID == "" || got["solo"].ResponseID == one {
		t.Fatalf("response identities: %v", got)
	}
}
