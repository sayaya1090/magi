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

// runOneToolCall drives one tool call through the real dispatch path and returns the tool result
// the model would see.
func runOneToolCall(t *testing.T, tool, args string) string {
	t.Helper()
	llm := &fakeLLM{steps: [][]port.ProviderEvent{toolStep(tool, args), textStep("done")}}
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "go"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})

	var out string
	for e := range ch {
		if e.Type == event.TypePartAppended {
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				var s string
				if json.Unmarshal(d.Part.ToolResult.Content, &s) != nil {
					s = string(d.Part.ToolResult.Content)
				}
				out = s
			}
		}
		if e.Type == event.TypeTurnFinished {
			break
		}
	}
	return out
}

// A key that IS a declared key up to case and separators is a misspelling: the model meant to pass
// that argument, so running without it discards something the call is relying on and reports the
// result as if nothing were missing. Recorded shape: the todo list arrives as `.todos`, todowrite
// reads nothing, and the plan is written EMPTY — the exact opposite of the call, reported as
// success. The refusal must NAME the real key, or it is the same dead end as a bare "unknown tool"
// reply, which models re-issue verbatim.
func TestMisspelledArgumentIsRefusedWithTheRealName(t *testing.T) {
	out := runOneToolCall(t, "todowrite", `{".todos":[{"content":"a","status":"pending"}]}`)
	for _, want := range []string{"not run", "`.todos` is `todos`", "todowrite accepts: todos"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool result = %q\n  missing %q", out, want)
		}
	}
}

// A key matching nothing is a capability the tool does not have. Refusing those would break calls
// that work today (the single largest recorded group is a harmless `description` on bash), so the
// call RUNS — and the result says which part of it was not honored, on the same result, so the
// correction arrives attached to the evidence it explains.
func TestUnrecognizedArgumentRunsButSaysWhatWasIgnored(t *testing.T) {
	out := runOneToolCall(t, "grep", `{"pattern":"nothing-here","ignore_case":true}`)
	for _, want := range []string{"[ignored arguments]", "`ignore_case`", "ran WITHOUT it", "grep accepts:"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool result = %q\n  missing %q", out, want)
		}
	}
}

// The overwhelming majority of calls are clean, and they must stay silent — an annotation on a
// correct call is noise that trains the model to ignore the channel.
func TestCorrectArgumentsGetNoAnnotation(t *testing.T) {
	out := runOneToolCall(t, "grep", `{"pattern":"nothing-here","path":"."}`)
	if strings.Contains(out, "ignored arguments") || strings.Contains(out, "not run") {
		t.Errorf("a correct call was annotated: %q", out)
	}
}
