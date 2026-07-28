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
	Question string   `json:"question"`
	Complete flexBool `json:"complete"`
}

func (Council) Name() string { return "council" }
func (Council) Description() string {
	return "Ask the council to review the work so far. Three members read the SAME record — the commands magi " +
		"observed running and how they ended, the files changed this turn, and your own latest message — each " +
		"through a different lens (correctness, verification, completeness), and each answers in its own words. " +
		"Pass `question` to point them at what you want weighed (\"does this handle an empty input?\"); omit it " +
		"for a general review, and their reading is advice you may disagree with. " +
		"Pass `complete: true` to DECLARE THE TASK FINISHED: this is how a turn ends. The council then reads " +
		"the record as a finish and either accepts — the turn is over and your work is handed back — or tells " +
		"you what is not done yet, and you keep working. Do not declare completion to ask a question; declare " +
		"it when you believe the work is finished and are ready to be held to it."
}
func (Council) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"complete":{"type":"boolean"}}}`)
}

func (Council) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Council == nil {
		return errResult("", "no council is configured for this run"), nil
	}
	var a councilArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	out, err := env.Council(ctx, strings.TrimSpace(a.Question), bool(a.Complete))
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return okText("", "The council had nothing to add."), nil
	}
	return okText("", out), nil
}
