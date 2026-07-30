package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Remember contributes a learning (memory) to the shared team experience store
// for review (D13). Use it to capture conventions, pitfalls, or solution
// patterns worth sharing across the team's sessions.
type Remember struct{}

type rememberArgs struct {
	Text  string   `json:"text"`
	Tags  []string `json:"tags"`
	Scope string   `json:"scope"`
}

func (Remember) Name() string { return "remember" }
func (Remember) Description() string {
	return "Save something worth keeping. Provide concise 'text' and optional 'tags'. " +
		"'scope' selects WHERE and for HOW LONG:\n" +
		"  \"turn\"    — SOMETHING TO CHECK BEFORE YOU DECLARE THIS TASK FINISHED. magi hands it back " +
		"word for word at the finish, in front of the decision about whether the work is done — so " +
		"write it the moment you notice something you would want to re-read at that point:\n" +
		"             - a part of the request your current step does not touch (\"must also work when " +
		"the list is empty\")\n" +
		"             - something you knowingly deferred or stubbed (\"timeout still hardcoded\")\n" +
		"             - a fact that cost you real work and must not be derived twice: a value you " +
		"measured, a cause you proved, a dead end you ruled out. A long task is compacted as it runs, " +
		"so what you worked out in its first minutes may not be in front of you at its last.\n" +
		"             One call, and you never have to ask for it back. It is not a log of what you " +
		"did — that is what your reply is for.\n" +
		"  \"project\" (default) — a durable workspace learning, recallable in later sessions via recall_memory.\n" +
		"  \"global\"  — knowledge useful across all projects.\n" +
		"Do not include secrets."
}
func (Remember) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"scope":{"type":"string","enum":["turn","project","global"],"description":"turn (reminded before this turn ends), project (default), or global"}},"required":["text"]}`)
}

func (Remember) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a rememberArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Text) == "" {
		return errResult("", "text is required"), nil
	}
	scope := strings.TrimSpace(a.Scope)
	if scope != "" && scope != "turn" && scope != "project" && scope != "global" {
		return errResult("", "scope must be \"turn\", \"project\" or \"global\""), nil
	}
	// A turn note never leaves this session: it is handed straight back to the agent that wrote
	// it, before the turn can end. Nothing reads it in between.
	if scope == "turn" {
		if env.NoteForTurn == nil {
			return errResult("", "turn notes are not available in this run"), nil
		}
		if err := env.NoteForTurn(a.Text); err != nil {
			return errResult("", err.Error()), nil
		}
		return okText("", "noted — magi will hand this back at the finish, before you can declare this task done"), nil
	}
	if env.Propose == nil {
		return errResult("", "shared experience is not configured"), nil
	}
	if err := env.Propose(port.Contribution{
		Memories: []port.Memory{{Text: a.Text, Tags: a.Tags}},
		Source:   "agent",
		Scope:    scope,
	}); err != nil {
		return errResult("", err.Error()), nil
	}
	where := "project"
	if scope == "global" {
		where = "global"
	}
	return okText("", "saved to "+where+" memory (recallable via recall_memory)"), nil
}
