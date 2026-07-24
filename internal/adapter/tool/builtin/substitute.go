package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// SubstituteCheck registers an acceptance-check SUBSTITUTION: when a check's given command cannot run
// in this environment (a missing tool, wrong path, no permission, a different setup), the agent runs
// an EQUIVALENT command that verifies the same goal and declares it here instead of failing the work.
// The substitution is reviewed by the council at the turn's finish boundary; once approved it rewrites
// the stored check to the working command so the fix persists for the rest of the run. Available to
// every agent (solo and delegated worker) so both handle a broken check the same way.
type SubstituteCheck struct{}

type substituteArgs struct {
	Step     string `json:"step"`
	Original string `json:"original"`
	Command  string `json:"command"`
	Expect   string `json:"expect"`
	Reason   string `json:"reason"`
}

func (SubstituteCheck) Name() string { return "substitute_check" }
func (SubstituteCheck) Description() string {
	return "Substitute an acceptance check whose given command cannot run HERE (missing tool, wrong path, no " +
		"permission, different setup — NOT the deliverable being wrong). First RUN an equivalent command that " +
		"verifies the SAME goal and confirm it passes, THEN call this to register it: step (the plan step), original " +
		"(the original check command you are replacing), command (the working equivalent you ran), expect (optional " +
		"expected output), reason (why the original could not run). The council reviews it before your turn finishes " +
		"and, once approved, it replaces the stored check for the rest of the run. Do NOT use this to dodge a check " +
		"that genuinely fails — that is a real failure to report, not a substitution."
}
func (SubstituteCheck) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"step":{"type":"string"},"original":{"type":"string"},"command":{"type":"string"},"expect":{"type":"string"},"reason":{"type":"string"}},"required":["command"]}`)
}

func (SubstituteCheck) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.SubstituteCheck == nil {
		return errResult("", "check substitution is not available here"), nil
	}
	var a substituteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Command) == "" {
		return errResult("", "command is required: the equivalent command you ran to verify the goal"), nil
	}
	if err := env.SubstituteCheck(port.CheckSub{
		Step: a.Step, Original: a.Original, Command: a.Command, Expect: a.Expect, Reason: a.Reason,
	}); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Substitution registered for step "+strings.TrimSpace(a.Step)+
		". The council will review it before your turn finishes; keep working or report done."), nil
}
