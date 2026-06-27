package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Remember contributes a learning (memory) to the shared team experience store
// for review (D13). Use it to capture conventions, pitfalls, or solution
// patterns worth sharing across the team's sessions.
type Remember struct{}

type rememberArgs struct {
	Text string   `json:"text"`
	Tags []string `json:"tags"`
}

func (Remember) Name() string { return "remember" }
func (Remember) Description() string {
	return "Save a durable learning (convention, pitfall, solution pattern) to the shared team memory for review. Provide concise 'text' and optional 'tags'. Do not include secrets."
}
func (Remember) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}}},"required":["text"]}`)
}

func (Remember) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Propose == nil {
		return errResult("", "shared experience is not configured"), nil
	}
	var a rememberArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Text) == "" {
		return errResult("", "text is required"), nil
	}
	if err := env.Propose(port.Contribution{
		Memories: []port.Memory{{Text: a.Text, Tags: a.Tags}},
		Source:   "agent",
	}); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "saved to shared memory (pending review)"), nil
}
