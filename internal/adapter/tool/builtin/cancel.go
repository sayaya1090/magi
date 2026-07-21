package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// CancelDispatch lets the ORCHESTRATOR stop still-running BACKGROUND subagents once an
// intermediate result has made the rest of a parallel wave unnecessary. Nothing is
// auto-rolled back: because the subagents share the workspace and may have run commands,
// touched the network, or started services, the system cannot safely undo one worker's
// changes in isolation. Instead each cancelled subagent's injected result carries a
// manifest of what it did, and the orchestrator is responsible for running any
// compensating (undo) actions itself. Offered only to the top-level orchestrator
// (env.CancelDispatch is nil for leaf subagents).
type CancelDispatch struct{}

type cancelDispatchArgs struct {
	Agent  string `json:"agent"`
	Reason string `json:"reason"`
}

func (CancelDispatch) Name() string { return "cancel_dispatch" }
func (CancelDispatch) Description() string {
	return "Cancel still-running background subagents you dispatched when an intermediate result made the REST " +
		"of the parallel wave unnecessary. Pass a one-line 'reason'; optional 'agent' to cancel only that role " +
		"(default: all remaining). Use ONLY after seeing a result that settles the question. Nothing is rolled " +
		"back automatically — each cancelled result lists what it already did, and YOU must undo any side effects " +
		"that must not persist before you rely on the workspace."
}
func (CancelDispatch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"agent":{"type":"string","description":"optional role to cancel; omit to cancel all remaining"},"reason":{"type":"string","description":"one line: why the remaining subagents are no longer needed"}},"required":["reason"]}`)
}

func (CancelDispatch) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.CancelDispatch == nil {
		return errResult("", "cancel_dispatch is only available to the orchestrator"), nil
	}
	var a cancelDispatchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Reason) == "" {
		return errResult("", "reason is required (why the remaining subagents are no longer needed)"), nil
	}
	n, err := env.CancelDispatch(strings.TrimSpace(a.Agent), strings.TrimSpace(a.Reason))
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if n == 0 {
		return okText("", "No running background subagents to cancel."), nil
	}
	return okText("", fmt.Sprintf("Cancelled %d subagent(s). Each one's result now carries the actions it "+
		"performed before cancel — review them and undo (compensate for) any side effects that must not persist, "+
		"then synthesize from the results you kept.", n)), nil
}
