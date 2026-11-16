package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Council asks the council for a reading of the work so far. It used to convene by itself at the
// turn's finish boundary, which meant it answered a question nobody had asked at the one moment the
// agent had already decided — and its answer arrived as the turn was ending, so nothing read it.
// Offered as a tool, it is asked when the agent wants another pair of eyes, and the answer comes
// back where every other tool result does: into the conversation, in time to be used.
//
// Each member sees the same record — what magi observed running, what changed, what the agent said —
// through a different lens, so the readings differ by viewpoint rather than by evidence.
type Council struct{}

type councilArgs struct {
	Question    string   `json:"question"`
	Complete    FlexBool `json:"complete"`
	ReportReady FlexBool `json:"report_ready"`
}

func (Council) Name() string { return "council" }
func (Council) Description() string {
	return "Ask the council to review the work so far: three members (correctness, verification, completeness) read the same record — the commands magi observed and how they ended, the files changed this turn, and your latest message. Pass `question` to point them at something; without `complete` their reading is advice you may disagree with. Pass `complete: true` to DECLARE THE TASK FINISHED for work done with tools in the current turn (a conversational answer with no tool work ends without this tool; earlier turns' work needs no new review). The council then accepts — the turn ends and your work is handed back — or says what is not done, and you keep working. Declare only when you are ready to be held to it, never to ask a question. Write your final user-facing report first: `report_ready` defaults to true, and an accepted report stands with no further reply. Set report_ready false only when the final report still needs to be written."
}
func (Council) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"complete":{"type":"boolean"},"report_ready":{"type":"boolean","default":true,"description":"The latest assistant message is the final report for the user; on acceptance do not regenerate it."}}}`)
}

func (Council) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Council == nil {
		return errResult("", "no council is configured for this run"), nil
	}
	a := councilArgs{ReportReady: true}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	out, err := env.Council(ctx, strings.TrimSpace(a.Question), bool(a.Complete), bool(a.ReportReady))
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return okText("", "The council had nothing to add."), nil
	}
	return okText("", out), nil
}
