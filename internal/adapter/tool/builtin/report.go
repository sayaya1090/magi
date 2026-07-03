package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/selfcheck"
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
	// A "done" report whose own text admits the work is a stand-in is not done. Refuse it
	// inline so the subagent gets immediate feedback and its turn continues, rather than
	// filing a fabricated result the orchestrator would take at face value. This scans the
	// report narrative; the loop separately scans the files the subagent wrote (both share
	// internal/core/selfcheck). "blocked"/"failed" are honest outcomes and pass through.
	//
	// This phrase scan is a best-effort, English-only pre-flag, NOT the authority (see the
	// SCOPE note on selfcheck.FabricationMarkers): a confession in another language or a
	// paraphrase passes through here. The behavioral backstop is at the top level — when the
	// PARENT turn finishes, the review gate's tester actually runs the merged deliverable and
	// the fresh-evidence gate (internal/app/loop.go) blocks completion on a real FAIL — so a
	// subagent's fabricated "done" that slips past this scan is still caught by real execution.
	if status == "done" {
		if _, line := selfcheck.FabricationMarker(a.Summary + "\n" + a.Details); line != "" {
			return errResult("", "This report says done, but your summary/details admit the work is a stand-in — "+
				"matched: \""+line+"\". A simulated or placeholder result is not a completed task. Do the real work "+
				"and report the genuine result, or use status \"failed\" and say what stopped you — do not report "+
				"fabricated work as done."), nil
		}
	}
	if err := env.Report(a.Summary, status, a.Details); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", "Report filed (status: "+status+"). Your turn is complete."), nil
}
