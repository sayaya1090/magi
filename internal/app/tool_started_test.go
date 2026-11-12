package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A crash between "the tool ran" and "its result was saved" must be told apart from a crash before
// it ran: the first may have changed the world and cannot simply be run again. So the log records,
// durably and BEFORE the tool executes, that this call started — and the result follows it.
func TestAToolCallIsRecordedAsStartedBeforeItsResult(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("glob", `{"pattern":"*"}`)}}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	runToTerminal(t, a, sid)

	evs, err := store.Read(ctx, sid, 0) // what survives a crash: the log on disk, not the bus
	if err != nil {
		t.Fatal(err)
	}
	started, result := -1, -1
	for i, e := range evs {
		switch e.Type {
		case event.Type("tool.started"):
			var d struct {
				CallID string `json:"callId"`
				Tool   string `json:"tool"`
			}
			if json.Unmarshal(e.Data, &d) == nil && d.CallID == "c_glob" && d.Tool == "glob" {
				started = i
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == "c_glob" {
				result = i
			}
		}
	}
	if result < 0 {
		t.Fatalf("the tool never produced a result; types=%v", typesOf(evs))
	}
	if started < 0 || started > result {
		t.Fatalf("tool.started for c_glob should be logged before its result: started=%d result=%d types=%v", started, result, typesOf(evs))
	}
}
