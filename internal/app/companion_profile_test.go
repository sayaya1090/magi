package app

import (
	"context"
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

func TestCompanionProfileRequiresSuccessfulReadEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []event.Event
		want   bool
	}{
		{"text only", saidEvents("I understand the project"), false},
		{"listing", []event.Event{evToolCall("a", "list"), evToolResult("a", "files", false)}, false},
		{"failed read", []event.Event{evToolCall("a", "read"), evToolResult("a", "missing", true)}, false},
		{"read", []event.Event{evToolCall("a", "read"), evToolResult("a", "project docs", false)}, true},
		{"large read", []event.Event{evToolCall("a", "read"), evToolResult("a", strings.Repeat("가", 10000), false)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := companionProfileEvidence(tc.events)
			if (out != "") != tc.want {
				t.Fatalf("evidence present=%v want=%v", out != "", tc.want)
			}
			if len(out) > 24<<10 {
				t.Fatal("unbounded evidence")
			}
		})
	}
}

func TestCompanionProfileRunsOnceOutsideTurnAndRespectsDisable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		store, err := jsonl.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		llm := &acCapLLM{text: `{"purpose":"workspace","tasks":[{"id":"build","description":"build"}]}`}
		saved := make(chan string, 2)
		a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Model: session.ModelRef{Model: "model"}, CompanionProfileNeeded: func(string) bool { return enabled }, CompanionProfileSave: func(_ string, p string) error { saved <- p; return nil }}))
		sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.store.Append(context.Background(), sid, evToolCall("a", "read"), evToolResult("a", "project docs", false))
		if err != nil {
			t.Fatal(err)
		}
		a.scheduleCompanionProfile(sid)
		a.scheduleCompanionProfile(sid)
		if enabled {
			select {
			case <-saved:
			case <-time.After(3 * time.Second):
				t.Fatal("profile was not extracted")
			}
			if len(llm.request().Tools) != 0 {
				t.Fatal("extraction must not have tools")
			}
		} else {
			time.Sleep(150 * time.Millisecond)
			if llm.called() {
				t.Fatal("disabled extraction called model")
			}
		}
	}
}

type profileLifecycleLLM struct {
	started chan struct{}
	block   bool
}

func (p profileLifecycleLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if req.System != companionProfilePrompt {
		return (completingLLM{}).StreamChat(ctx, req)
	}
	close(p.started)
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return (&acCapLLM{text: `{"purpose":"workspace","tasks":[{"id":"test","description":"run project tests"}]}`}).StreamChat(ctx, req)
}

func TestCompanionProfileIsTriggeredByCompletedTurnAndCanBeCancelled(t *testing.T) {
	for _, block := range []bool{false, true} {
		store, err := jsonl.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		llm := profileLifecycleLLM{started: make(chan struct{}), block: block}
		saved := make(chan string, 1)
		a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{CompanionProfileNeeded: func(string) bool { return true }, CompanionProfileSave: func(_ string, p string) error { saved <- p; return nil }}))
		sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.store.Append(context.Background(), sid, evToolCall("a", "read"), evToolResult("a", "project docs", false)); err != nil {
			t.Fatal(err)
		}
		if err = a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hello"}}, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-llm.started:
		case <-time.After(5 * time.Second):
			t.Fatal("completed turn did not trigger extraction")
		}
		if block {
			a.CancelCompanionProfile()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = a.Close(ctx); err != nil {
				t.Fatal("cancelled extraction leaked", err)
			}
			select {
			case <-saved:
				t.Fatal("cancelled result was published")
			default:
			}
		} else {
			select {
			case <-saved:
			case <-time.After(time.Second):
				t.Fatal("result was not published")
			}
		}
	}
}
