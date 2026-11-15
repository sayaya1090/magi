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
