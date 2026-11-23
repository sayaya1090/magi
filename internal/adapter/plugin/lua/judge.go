package lua

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	lua "github.com/yuin/gopher-lua"
)

// This plugin-owned tool can run under the caller's plugin lock: it never enters a Lua state.
type judgeTool struct {
	plugin *plugin
	model  string
}

func (p *plugin) bridgeRegisterJudge(L *lua.LState) int {
	p.requireCap(L, "tool")
	p.requireCap(L, "analyze")
	model, _ := p.host.pluginConfigs[p.name]["judge_model"].(string)
	p.tools = append(p.tools, &judgeTool{plugin: p, model: model})
	return 0
}
func (*judgeTool) Name() string { return "judge" }
func (*judgeTool) Description() string {
	return "Make one bounded decision about supplied evidence. Give a question and either string choices or a JSON schema. Returns {answer: value}. Prefer deterministic rules when possible."
}
func (*judgeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"choices":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":32},"schema":{"type":"object"}},"required":["question"],"additionalProperties":false}`)
}
func (t *judgeTool) Execute(ctx context.Context, raw json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	var args struct {
		Question string
		Choices  []string
		Schema   json.RawMessage
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 16384 || !json.Valid(raw) || decoder.Decode(&args) != nil || strings.TrimSpace(args.Question) == "" || (args.Choices == nil) == (args.Schema == nil) {
		return errResult("judge requires a question and exactly one of choices or schema (16 KiB total)"), nil
	}
	contract := args.Schema
	tokens := 512
	if args.Choices != nil {
		if len(args.Choices) == 0 || len(args.Choices) > 32 {
			return errResult("judge expects 1 to 32 choices"), nil
		}
		for i, c := range args.Choices {
			if strings.TrimSpace(c) == "" || len(c) > 256 || slices.Contains(args.Choices[:i], c) {
				return errResult("judge choices must be distinct nonempty strings of at most 256 bytes"), nil
			}
		}
		if len(args.Choices) == 1 {
			b, _ := json.Marshal(map[string]string{"answer": args.Choices[0]})
			return session.ToolResult{Content: b}, nil
		}
		contract, _ = json.Marshal(map[string]any{"enum": args.Choices})
		tokens = 128
	}
	if len(contract) == 0 || contract[0] != '{' {
		return errResult("judge expects a JSON schema object"), nil
	}
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(string) (io.ReadCloser, error) { return nil, errors.New("external schemas disabled") }
	const id = "urn:magi:judge"
	if err := compiler.AddResource(id, bytes.NewReader(contract)); err != nil {
		return errResult("judge schema: " + err.Error()), nil
	}
	schema, err := compiler.Compile(id)
	if err != nil {
		return errResult("judge schema: " + err.Error()), nil
	}
	if t.plugin.host.analyzer == nil {
		return errResult("judge: no analyzer available"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	out, _, err := t.plugin.runAnalysis(ctx, analyzeRequest{system: "Return only a JSON value satisfying this schema: " + string(contract), text: args.Question, model: t.model, label: "judge", maxOutputTokens: tokens})
	if err != nil {
		return errResult("judge: " + err.Error()), nil
	}
	if len(out) > 16384 || !json.Valid([]byte(out)) {
		return errResult("judge: expected one JSON value"), nil
	}
	var answer any
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&answer); err != nil {
		return errResult("judge: " + err.Error()), nil
	}
	if err := schema.Validate(answer); err != nil {
		return errResult("judge: answer outside contract"), nil
	}
	result, _ := json.Marshal(map[string]json.RawMessage{"answer": json.RawMessage(out)})
	return session.ToolResult{Content: result}, nil
}
