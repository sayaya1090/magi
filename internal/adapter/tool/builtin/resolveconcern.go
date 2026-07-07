package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// ResolveConcern lets the ORCHESTRATOR retire a structural concern from the durable
// ledger once it has genuinely handled it — the premise the concern flagged was verified,
// or the plan pivoted so the concern no longer applies. It is advisory-only: a concern
// that is STILL TRUE is re-raised deterministically on the next turn (the ledger
// self-heals), so this can clear accumulated advisory memory but can never launder away a
// fact that remains true. Offered only to the top-level orchestrator (env.ResolveConcern
// is nil for leaf subagents, which lack the whole-task view a reset needs).
type ResolveConcern struct{}

type resolveConcernArgs struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

func (ResolveConcern) Name() string { return "resolveconcern" }
func (ResolveConcern) Description() string {
	return "Retire a structural concern from the ledger AFTER you have genuinely resolved it — e.g. you " +
		"verified the premise it flagged, or the plan changed so it no longer applies. Pass the exact key " +
		"(as shown in the council evidence) and a one-line reason. This clears ONLY advisory memory: if the " +
		"underlying issue is still real it is re-raised automatically next turn, so do NOT use it to silence a " +
		"concern you have not actually addressed."
}
func (ResolveConcern) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"exact concern key to retire"},"reason":{"type":"string","description":"one line: how it was resolved"}},"required":["key","reason"]}`)
}

func (ResolveConcern) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.ResolveConcern == nil {
		return errResult("", "resolveconcern is only available to the orchestrator"), nil
	}
	var a resolveConcernArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	key := strings.TrimSpace(a.Key)
	if key == "" {
		return errResult("", "key is required (the exact concern key to retire)"), nil
	}
	if err := env.ResolveConcern(key, strings.TrimSpace(a.Reason)); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Concern \""+key+"\" retired. If the underlying issue is still real it will be re-raised next turn."), nil
}
