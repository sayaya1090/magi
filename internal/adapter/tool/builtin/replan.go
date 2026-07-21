package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Replan lets a plan-eligible agent declare that its current plan is unworkable given
// what the work has actually produced, and get a fresh decomposition plus a reset
// no-progress window — instead of thrashing into the stall guard. It is budget-capped
// per turn (and refused if you replan again without acting in between), so it cannot be
// used to dodge the stall guard indefinitely. Offered only to plan-eligible agents
// (env.Replan is nil for read-only or max-depth agents, which cannot re-plan).
type Replan struct{}

type replanArgs struct {
	Reason string `json:"reason"`
}

func (Replan) Name() string { return "replan" }
func (Replan) Description() string {
	return "Declare that your current plan CANNOT proceed given what the work has shown (a premise broke, the " +
		"approach is invalidated) and request a fresh decomposition. Resets the plan and the no-progress window so " +
		"a new approach isn't instantly force-stopped. Use sparingly, only for a genuine dead end — NOT a " +
		"transient error you can retry. Budget-capped per turn and refused if you call it again without taking " +
		"real action in between. Give a one-line reason."
}
func (Replan) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"one line: why the current plan cannot proceed"}},"required":["reason"]}`)
}

func (Replan) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Replan == nil {
		return errResult("", "replan is not available to this agent"), nil
	}
	var a replanArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Reason) == "" {
		return errResult("", "reason is required (one line: why the current plan cannot proceed)"), nil
	}
	if err := env.Replan(strings.TrimSpace(a.Reason)); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Replan requested. If accepted, your plan and no-progress window reset next step — decompose a "+
		"fresh approach and proceed. If the per-turn budget is exhausted, you'll be told to continue with the current plan instead."), nil
}
