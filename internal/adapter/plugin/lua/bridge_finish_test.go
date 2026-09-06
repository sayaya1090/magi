package lua

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

func finishPlugin(t *testing.T, finish func(string)) port.Tool {
	t.Helper()
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Finish: finish, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"ender\"\ncapabilities=[\"tool\",\"notify\"]\n", `
magi.register_tool{ name = "end_it", description = "d",
  schema = { type = "object", properties = { who = { type = "string" } } },
  execute = function(args)
    local ok, err = magi.finish(args.who)
    if not ok then return "ERR " .. tostring(err), true end
    return "ended"
  end }
`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	tool, ok := sink.Get("end_it")
	if !ok {
		t.Fatal("the plugin registered no tool")
	}
	return tool
}

// magi.finish ends the turn the calling tool runs in: the host's Finish callback gets THAT session,
// from the tool call's env, not from whatever the plugin says.
func TestFinishEndsTheCallingToolsTurn(t *testing.T) {
	var ended []string
	tool := finishPlugin(t, func(sid string) { ended = append(ended, sid) })
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{SessionID: "s_1"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	_ = json.Unmarshal(res.Content, &got)
	if got != "ended" || len(ended) != 1 || ended[0] != "s_1" {
		t.Fatalf("finish did not end the calling session: got=%q ended=%v", got, ended)
	}
	// Naming another session is refused — a tool ends its own turn, nobody else's.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{"who":"s_2"}`), port.ToolEnv{SessionID: "s_1"})
	_ = json.Unmarshal(res.Content, &got)
	if !res.IsError || !strings.Contains(got, "not the session") || len(ended) != 1 {
		t.Errorf("another session's turn was ended, or the refusal is unclear: %q ended=%v", got, ended)
	}
	// Outside a tool call there is no turn to end.
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
	_ = json.Unmarshal(res.Content, &got)
	if !res.IsError || !strings.Contains(got, "inside a tool call") {
		t.Errorf("without a session the bridge must say so, got %q", got)
	}
}

// Without a Finish door on the host (an older daemon), the bridge says so rather than pretending.
func TestFinishSaysSoWhenTheHostHasNoDoor(t *testing.T) {
	tool := finishPlugin(t, nil)
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{SessionID: "s_1"})
	var got string
	_ = json.Unmarshal(res.Content, &got)
	if !res.IsError || !strings.Contains(got, "no turn to end") {
		t.Errorf("a host without the door must refuse plainly, got %q", got)
	}
}

// The landing plugin's land, on a landing it accepts, ends the turn.
func TestLandingLandEndsTheTurnItAccepts(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "landing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "init.lua")); err != nil {
		t.Skip("plugins/landing not beside this tree")
	}
	sink := builtin.NewRegistry()
	var ended []string
	h := NewHostWithConfig(HostConfig{ToolSink: sink, ContextReg: &fakeContextReg{},
		Notify: func(string, string) {}, Finish: func(sid string) { ended = append(ended, sid) }, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load landing: %v", err)
	}
	land, ok := sink.Get("land")
	if !ok {
		t.Fatal("landing registered no land tool")
	}
	env := port.ToolEnv{SessionID: "s_land", TurnSteps: func(context.Context) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "set_text"}}, nil
	}}
	res, err := land.Execute(context.Background(), json.RawMessage(`{"did":["바꾼 것 없음"]}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("an honest nothing-changed landing was refused: %s", res.Content)
	}
	if len(ended) != 1 || ended[0] != "s_land" {
		t.Errorf("land accepted but did not end the turn: %v", ended)
	}
	// A refused landing does not end anything.
	res, _ = land.Execute(context.Background(), json.RawMessage(`{"did":[]}`), env)
	if !res.IsError || len(ended) != 1 {
		t.Errorf("a refused landing ended the turn: err=%v ended=%v", res.IsError, ended)
	}
}
