package lua

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestJudgeStandaloneAndInsideItsOwningScript(t *testing.T) {
	analyzer := &fakeAnalyzer{reply: `"review"`}
	reg := builtin.NewRegistry()
	host := NewHostWithConfig(HostConfig{ToolSink: reg, Analyzer: analyzer, DataDir: t.TempDir()})
	ctx := context.Background()
	if _, err := host.Load(ctx, "../../../../plugins/dummy-plug"); err != nil {
		t.Fatal(err)
	}
	judge, ok := reg.Get("judge")
	if !ok {
		t.Fatal("missing standalone tool")
	}
	run := func(raw string, wantError bool, calls int) session.ToolResult {
		t.Helper()
		result, err := judge.Execute(ctx, json.RawMessage(raw), port.ToolEnv{})
		if err != nil || result.IsError != wantError || analyzer.calls != calls {
			t.Fatalf("%s %v calls=%d", result.Content, err, analyzer.calls)
		}
		return result
	}
	if got := run(`{"question":"classify","choices":["keep"]}`, false, 0); string(got.Content) != `{"answer":"keep"}` {
		t.Fatal(string(got.Content))
	}
	run(`{"question":"classify","choices":["keep","drop"]}`, true, 1)
	run(`{"question":"classify","choices":["keep","keep"]}`, true, 1)
	run(`{"question":"extract","schema":{"$ref":"file:///forbidden"}}`, true, 1)
	const extract = `{"question":"extract","schema":{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"],"additionalProperties":false}}`
	analyzer.reply = `{"count":"3"}`
	run(extract, true, 2)
	analyzer.reply = `{"count":3}`
	run(extract, false, 3)
	analyzer.reply = `false`
	tool, _ := reg.Get("dummy_plug")
	env := port.ToolEnv{Workdir: t.TempDir(), CallTool: func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		if name != "judge" {
			t.Fatalf("unexpected tool %s", name)
		}
		return judge.Execute(ctx, args, port.ToolEnv{})
	}}
	raw, _ := json.Marshal(map[string]string{"goal": "check", "script": `local v,e=judge("is it ready?",{type="boolean"}); assert(e==nil and v==false); print("pending")`})
	result, err := tool.Execute(ctx, raw, env)
	if err != nil || result.IsError || !strings.Contains(string(result.Content), "pending") || analyzer.calls != 4 {
		t.Fatalf("%s %v calls=%d", result.Content, err, analyzer.calls)
	}
	if err := host.Unload("dummy-plug"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("judge"); ok {
		t.Fatal("judge survived owner unload")
	}
}
func TestJudgeCandidatesInspectExistingBranchesWithoutAModel(t *testing.T) {
	code := `for _,item in ipairs(items) do
 local answer = call_tool("explore",{question=item.question})
 if answer == "accept" then print(item) end
end`
	if lines := judgeCandidates(code); len(lines) != 1 || lines[0] != 2 {
		t.Fatalf("got %v", lines)
	}
	if lines := judgeCandidates(strings.Replace(code, "if answer", "if unrelated", 1)); len(lines) != 0 {
		t.Fatal(lines)
	}
}
