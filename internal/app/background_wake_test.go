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

// bgTool starts "background work": it registers the completion channel and returns at once.
type bgTool struct {
	done chan port.BackgroundCompletion
}

func (t *bgTool) Name() string            { return "bgjob" }
func (t *bgTool) Description() string     { return "starts work in the background" }
func (t *bgTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *bgTool) Execute(_ context.Context, _ json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.ObserveBackground != nil {
		env.ObserveBackground(t.done)
	}
	return session.ToolResult{Content: json.RawMessage(`"started bg_1"`), Background: true}, nil
}

func bgSession(t *testing.T) (*App, session.SessionID, *bgTool) {
	t.Helper()
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	tool := &bgTool{done: make(chan port.BackgroundCompletion, 1)}
	reg.Register(tool)
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("bgjob", `{}`), asideText("done"), asideText("noted the result")}}
	a := closeAfter(t, New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: true}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "build it in the background"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, a, sid)
	return a, sid, tool
}

func countTurnsAndNotes(t *testing.T, a *App, sid session.SessionID) (turns, notes int, note string) {
	t.Helper()
	evs, _ := a.store.Read(context.Background(), sid, 0)
	for _, e := range evs {
		switch e.Type {
		case event.TypeTurnFinished:
			turns++
		case event.TypePromptSubmitted:
			if e.Actor.Kind == event.ActorSystem && e.Actor.ID == "background" {
				notes++
				note = string(e.Data)
			}
		}
	}
	return
}

// Background work comes back to the agent (2026-11-17: "when the background wait ends the result
// should reach you and you carry on"). A job this conversation started that ends while nothing is
// running leaves a note for the model and starts a turn, so the model reads the result and continues
// instead of waiting for the person to ask.
func TestAFinishedBackgroundJobWakesTheAgent(t *testing.T) {
	a, sid, tool := bgSession(t)
	tool.done <- port.BackgroundCompletion{Exit: 2}
	close(tool.done)
	var turns, notes int
	var note string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if turns, notes, note = countTurnsAndNotes(t, a, sid); turns >= 2 && notes == 1 {
			break
		}
	}
	if notes != 1 || turns < 2 {
		t.Fatalf("the finished job did not come back: %d notes, %d turns", notes, turns)
	}
	if !strings.Contains(note, "failed") || !strings.Contains(note, "exit 2") {
		t.Errorf("the note must say how it ended: %s", note)
	}
}

// A job magi killed on purpose (bash_kill) is not news to the agent that killed it.
func TestAKilledBackgroundJobDoesNotWakeTheAgent(t *testing.T) {
	a, sid, tool := bgSession(t)
	tool.done <- port.BackgroundCompletion{Killed: true}
	close(tool.done)
	time.Sleep(500 * time.Millisecond)
	if turns, notes, _ := countTurnsAndNotes(t, a, sid); notes != 0 || turns != 1 {
		t.Fatalf("a killed job woke the agent: %d notes, %d turns", notes, turns)
	}
}

// The person's message, steered into a conversation, ends any wait on it (ToolEnv.PromptArrived).
func TestAMessageWakesAWaitOnItsConversation(t *testing.T) {
	a, sid, _ := bgSession(t)
	wake := a.promptArrived(sid)
	if err := a.Steer(context.Background(), command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "how is it going?"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(2 * time.Second):
		t.Fatal("the message did not wake the wait")
	}
}
