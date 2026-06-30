package builtin

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// RecallContext re-hydrates detail that an earlier compaction shed from context. When
// the conversation is compacted, a notice lists recoverable topics; the agent calls this
// to pull one back verbatim instead of working from the lossy summary. (recall capability)
type RecallContext struct{}

type recallArgs struct {
	Topic string `json:"topic"`
}

func (RecallContext) Name() string { return "recall_context" }
func (RecallContext) Description() string {
	return "Retrieve the full original detail of an earlier topic that was compacted out of context. " +
		"After a compaction notice lists recoverable topics, pass {topic} — a topic name or keywords " +
		"(a file path works well, e.g. recall_context with topic \"internal/app/loop.go\"). " +
		"Returns the original messages verbatim; if nothing matches, returns the list of available topics."
}
func (RecallContext) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","description":"topic name or keywords to recall"}},"required":["topic"]}`)
}

func (RecallContext) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Recall == nil {
		return errResult("", "context recall is not available here"), nil
	}
	var a recallArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.Topic == "" {
		return errResult("", "provide a {topic} to recall"), nil
	}
	out, err := env.Recall(a.Topic)
	if err != nil {
		return errResult("", "recall failed: "+err.Error()), nil
	}
	return okText("", out), nil
}
