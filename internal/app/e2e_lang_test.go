package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A Korean prompt must yield a Korean reply from the live model (language lock).
func TestE2ELanguageLockKorean(t *testing.T) {
	base := os.Getenv("MAGI_E2E_OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	if base == "disabled" || !reachable(base) {
		t.Skipf("ollama not reachable at %s", base)
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	store, _ := jsonl.New(t.TempDir())
	a := New(store, openai.New(base, os.Getenv("MAGI_E2E_API_KEY")), builtin.Default(), bus.New(), platform.New(), Config{
		Model:      session.ModelRef{Provider: "openai", Model: model},
		Permission: "allow",
		System:     "You are a helpful assistant.",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: model}})
	sub, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "한 문장으로 자기소개를 해 줘."}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	reply := ""
	finished := false
	deadline := time.After(90 * time.Second)
	for !finished {
		select {
		case e, ok := <-sub:
			if !ok {
				finished = true
				break
			}
			if e.Type == event.TypePartAppended {
				var d event.PartAppendedData
				if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartText {
					reply += d.Part.Text
				}
			}
			if e.Type == event.TypeTurnFinished {
				finished = true
			}
		case <-deadline:
			finished = true
		}
	}
	if langDirective(reply) == "" || !contains(langDirective(reply), "Korean") {
		t.Errorf("expected a Korean reply, got: %q", reply)
	} else {
		t.Logf("got Korean reply: %q", reply)
	}
}
