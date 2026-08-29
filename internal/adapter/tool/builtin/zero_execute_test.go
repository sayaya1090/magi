package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Every registered tool must present a complete face: a name, words, and a schema that parses.
// This is the one test that covers the getters wholesale — a tool with a blank face is a tool the
// model cannot use correctly.
func TestEveryToolPresentsACompleteFace(t *testing.T) {
	r := Default()
	RegisterOrchestration(r, false)
	for _, tool := range r.List() {
		if strings.TrimSpace(tool.Name()) == "" || strings.TrimSpace(tool.Description()) == "" {
			t.Errorf("%T has a blank face", tool)
		}
		var v any
		if err := json.Unmarshal(tool.Schema(), &v); err != nil {
			t.Errorf("%s: schema does not parse: %v", tool.Name(), err)
		}
	}
}

// The three env-closure tools refuse a run that lacks their capability IN WORDS, and pass their
// arguments through whole when it is there.
func TestEnvClosureToolsRefuseAndMap(t *testing.T) {
	ctx := context.Background()

	if res, err := (Council{}).Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{}); err != nil || !res.IsError {
		t.Fatalf("no council configured: an error result, not a tool error: (%+v, %v)", res, err)
	}
	var gotQ string
	var gotDone bool
	env := port.ToolEnv{Council: func(_ context.Context, q string, done bool) (string, error) {
		gotQ, gotDone = q, done
		return "", nil
	}}
	res, err := (Council{}).Execute(ctx, json.RawMessage(`{"question":"  am I done?  ","complete":true}`), env)
	if err != nil || res.IsError {
		t.Fatalf("council: (%+v, %v)", res, err)
	}
	if gotQ != "am I done?" || !gotDone {
		t.Fatalf("the question crosses trimmed and the declaration crosses whole: (%q, %v)", gotQ, gotDone)
	}
	if !strings.Contains(string(res.Content), "nothing to add") {
		t.Fatalf("an empty verdict is said, not blanked: %s", res.Content)
	}

	if res, _ := (Schedule{}).Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{}); !res.IsError {
		t.Fatal("no scheduler here must be said")
	}
	var got port.ScheduleChange
	senv := port.ToolEnv{Schedule: func(c port.ScheduleChange) (string, error) {
		got = c
		return "3 job(s)", nil
	}}
	res, _ = (Schedule{}).Execute(ctx, json.RawMessage(`{"action":"set","name":"tick","schedule":"@daily","prompt":"do it"}`), senv)
	if res.IsError || got.Action != "set" || got.Name != "tick" || got.Schedule != "@daily" || got.Prompt != "do it" {
		t.Fatalf("schedule mapping: (%+v, %+v)", res, got)
	}

	if res, _ := (SearchSessions{}).Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{}); !res.IsError {
		t.Fatal("no search here must be said")
	}
	qenv := port.ToolEnv{SearchSessions: func(q, open string) (string, error) { return "hit:" + q + open, nil }}
	if res, _ := (SearchSessions{}).Execute(ctx, json.RawMessage(`{}`), qenv); !res.IsError ||
		!strings.Contains(string(res.Content), "query") {
		t.Fatal("neither a query nor an open id is a request for nothing, and the refusal names both")
	}
	if res, _ := (SearchSessions{}).Execute(ctx, json.RawMessage(`{"query":"parser"}`), qenv); res.IsError ||
		!strings.Contains(string(res.Content), "hit:parser") {
		t.Fatalf("search: %+v", res)
	}
}
