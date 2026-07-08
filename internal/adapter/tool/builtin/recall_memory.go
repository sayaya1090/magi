package builtin

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// RecallMemory pulls durable team memories/skills (the shared experience store) that
// match a query, on demand. The system prompt only advertises how many relevant
// entries exist — the agent calls this to actually read them, so nothing is spent on
// context until it wants the detail. Distinct from recall_context, which recovers
// THIS session's compacted-out messages; recall_memory reaches the cross-session store
// that `remember` writes to.
type RecallMemory struct{}

type recallMemoryArgs struct {
	Query string `json:"query"`
}

func (RecallMemory) Name() string { return "recall_memory" }
func (RecallMemory) Description() string {
	return "Pull saved team memories/skills from the shared experience store by keyword. " +
		"Call this when the prompt notes that relevant entries exist, or whenever a past convention, " +
		"pitfall, or reusable procedure might apply to what you are doing now. Pass {query} — keywords " +
		"describing the current task. (Different from recall_context, which restores this session's own " +
		"compacted-out detail.)"
}
func (RecallMemory) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"keywords describing what you need to recall"}},"required":["query"]}`)
}

func (RecallMemory) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.RecallMemory == nil {
		return errResult("", "shared memory is not available here"), nil
	}
	var a recallMemoryArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.Query == "" {
		return errResult("", "provide a {query} — keywords describing what you need to recall"), nil
	}
	out, err := env.RecallMemory(a.Query)
	if err != nil {
		return errResult("", "recall failed: "+err.Error()), nil
	}
	if out == "" {
		return okText("", "No stored memories matched that query."), nil
	}
	return okText("", out), nil
}
