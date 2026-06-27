package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Ask lets a subagent escalate a question to its orchestrator mid-task and get an
// answer back, so it can request anything it's missing instead of guessing or
// failing. Only meaningful for subagents (the app wires ToolEnv.Ask for them).
type Ask struct{}

type askArgs struct {
	Question string `json:"question"`
}

func (Ask) Name() string { return "ask" }
func (Ask) Description() string {
	return "Ask your orchestrator for something you need to continue. It has the FULL context (the user's original " +
		"request, the overall plan, and other subagents' results) and can: clarify intent or make a decision; give " +
		"you the user's request details, file paths, or constraints; relay your question to the USER; and coordinate " +
		"with OTHER subagents (it routes peer questions, so ask it rather than guessing what another agent found). " +
		"Blocks until it replies and returns the answer. Use only when your own tools (read/grep/…) can't get it."
}
func (Ask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}`)
}

func (Ask) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Ask == nil {
		return errResult("", "ask is only available to subagents"), nil
	}
	var a askArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Question) == "" {
		return errResult("", "ask: 'question' is required"), nil
	}
	answer, err := env.Ask(a.Question)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", answer), nil
}
