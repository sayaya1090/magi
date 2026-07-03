package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Report is how a subagent delivers its FINAL result and ENDS its turn. It is the
// output side of the subagent contract (the input side is the task prompt; 'ask'
// requests more input mid-task). Giving the model one sanctioned way to "finish
// and return" stops weak models from echoing conclusions via bash and looping.
type Report struct{}

type reportArgs struct {
	Summary string `json:"summary"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

func (Report) Name() string { return "report" }
func (Report) Description() string {
	return "End your turn and hand your result back to the orchestrator. WRITE your actual answer/findings as your " +
		"normal message FIRST (it streams to the user live), THEN call this to finish. Fields: status = \"done\" " +
		"(finished), \"blocked\" (need something only the orchestrator can give — say what), or \"failed\" (attempted " +
		"but failed — say why); summary (optional — only if you did NOT already write your answer as your message); " +
		"details (optional). After reporting you stop — do NOT use bash/echo to present results."
}
func (Report) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["done","blocked","failed"]},"summary":{"type":"string"},"details":{"type":"string"}},"required":["status"]}`)
}

func (Report) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Report == nil {
		return errResult("", "report is only available to subagents"), nil
	}
	var a reportArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	status := strings.ToLower(strings.TrimSpace(a.Status))
	switch status {
	case "done", "blocked", "failed":
	case "":
		status = "done"
	default:
		status = "done" // normalize anything unexpected to done
	}
	// A subagent's fabricated "done" is caught behaviorally, not by scanning this narrative for
	// English confession phrases (which missed other languages and non-confessing fakes). The
	// report tool has no execution context, so the check lives where the tool log does: the
	// loop's take-report branch refuses a "done" report whose deliverable was changed but never
	// exercised (internal/app.runGuard.unverifiedDeliverable), and when the PARENT turn finishes
	// the review-gate tester runs the merged deliverable for real. Both are language-agnostic;
	// "blocked"/"failed" remain honest outcomes that always pass through.
	if err := env.Report(a.Summary, status, a.Details); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Report filed (status: "+status+"). Your turn is complete."), nil
}
