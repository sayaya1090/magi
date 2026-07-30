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
		"  \"turn\"    — a note to yourself for THIS task, handed back word for word before the turn " +
		"can end. A long task is compacted as it runs: what you worked out in its first minutes may " +
		"not be in front of you at its last, and working it out again costs what it cost the first " +
		"time. This costs one call, and you never have to ask for it back. Use it the moment you " +
		"establish something you must not derive twice — a value you measured, a cause you proved, " +
		"a dead end you ruled out, a constraint of the task your current step does not touch.\n" +
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
		return okText("", "noted for this turn — magi will hand this back before the turn ends"), nil
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
