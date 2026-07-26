package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// SubstituteCheck registers an acceptance-check SUBSTITUTION: when a check cannot be satisfied AS
// WRITTEN — it reads a path nothing in this task produces, or the real output was recorded elsewhere,
// or its assertion cannot prove the goal — the agent supplies the source/assert pair that does instead
// of leaving the check unmet. The substitution is reviewed by the council at the turn's finish
// boundary; once approved it rewrites the stored check so the fix persists for the rest of the run.
// Available to every agent (solo and delegated worker) so both handle a broken check the same way.
type SubstituteCheck struct{}

type substituteArgs struct {
	Step     string `json:"step"`
	Original string `json:"original"`
	Source   string `json:"source"`
	Assert   string `json:"assert"`
	Reason   string `json:"reason"`
}

func (SubstituteCheck) Name() string { return "substitute_check" }
func (SubstituteCheck) Description() string {
	return "Substitute an acceptance check you cannot satisfy AS WRITTEN — it reads a path nothing in this task " +
		"produces, or you recorded the real output somewhere else, or its assertion cannot prove the stated goal " +
		"(the CHECK is broken, NOT the deliverable). A check is data, not a command: the gate reads a `source` file " +
		"and applies an `assert` to it, so what you supply here is the pair that proves the same goal. Do NOT just " +
		"leave the item unmet and move on — that leaves the broken check in place and the step lands ungated. " +
		"Instead: first make sure the real output IS recorded at the path you are about to name, THEN call this: " +
		"step (the plan step), original (the check being replaced), source (the path holding the real output), " +
		"assert (one of: nonempty; matches <regexp>; absent <regexp>; equals <path>; port_open <port>; " +
		"process_alive), reason (why the given check cannot be satisfied). The council reviews it before your turn " +
		"finishes and, once approved, it replaces the stored check for the rest of the run. Do NOT use this to " +
		"dodge a check that genuinely fails (the deliverable is wrong) — that is a real failure to report, not a " +
		"substitution."
}
func (SubstituteCheck) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"step":{"type":"string"},"original":{"type":"string"},"source":{"type":"string"},"assert":{"type":"string"},"reason":{"type":"string"}},"required":["assert"]}`)
}

func (SubstituteCheck) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.SubstituteCheck == nil {
		return errResult("", "check substitution is not available here"), nil
	}
	var a substituteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Assert) == "" {
		return errResult("", "assert is required: the assertion that proves the same goal (nonempty; matches <regexp>; "+
			"absent <regexp>; equals <path>; port_open <port>; process_alive)"), nil
	}
	if err := env.SubstituteCheck(port.CheckSub{
		Step: a.Step, Original: a.Original, Source: a.Source, Assert: a.Assert, Reason: a.Reason,
	}); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Substitution registered for step "+strings.TrimSpace(a.Step)+
		". The council will review it before your turn finishes; keep working or report done."), nil
}
