package builtin

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Skill loads a named skill's instructions into the conversation so the agent
// can follow them. Skills are listed in the system prompt; the model calls this
// when a task matches one. (skill capability)
type Skill struct{}

type skillArgs struct {
	Name string `json:"name"`
}

func (Skill) Name() string { return "skill" }
func (Skill) Description() string {
	return "Load a named skill's full instructions (see the Available skills list). Provide {name}."
}
func (Skill) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
}

func (Skill) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.LoadSkill == nil {
		return errResult("", "skills are not available in this context"), nil
	}
	var a skillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	body, ok := env.LoadSkill(a.Name)
	if !ok {
		return errResult("", "unknown skill: "+a.Name), nil
	}
	return okText("", body), nil
}
