package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// AskUser presents the HUMAN user one or more multiple-choice questions and
// blocks for their picks — the structured alternative to burying options in
// prose the user has to re-type. Top-level interactive sessions only: the app
// wires ToolEnv.AskUser there and leaves it nil for subagents (which escalate
// via ask) and headless runs (which must not block on a human who isn't there).
type AskUser struct{}

type askUserQ struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

type askUserArgs struct {
	Questions []askUserQ `json:"questions"`
}

func (AskUser) Name() string { return "ask_user" }
func (AskUser) Description() string {
	return "Ask the USER to choose between concrete options (a selection modal, one question at a time). Use it " +
		"when a decision is genuinely the user's to make — approach, scope, naming, destructive vs safe variant — " +
		"and the options are real alternatives you can enumerate (2-4 each, short labels). Do NOT use it for " +
		"decisions with an obvious default, or to ask permission the tool system already handles. Each answer is " +
		"the chosen option's text; an empty answer means the user dismissed the question — proceed on your best " +
		"judgment and say so."
}
func (AskUser) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"questions":{"type":"array","items":{"type":"object",` +
		`"properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}}},` +
		`"required":["question","options"]},"minItems":1,"maxItems":4}},"required":["questions"]}`)
}

func (AskUser) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.AskUser == nil {
		return errResult("", "ask_user is unavailable here (no interactive user) — decide on your own best judgment and state the assumption"), nil
	}
	var a askUserArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if len(a.Questions) == 0 {
		return errResult("", "ask_user: 'questions' is required"), nil
	}
	var b strings.Builder
	for i, q := range a.Questions {
		if strings.TrimSpace(q.Question) == "" || len(q.Options) < 2 {
			return errResult("", fmt.Sprintf("ask_user: question %d needs text and at least 2 options", i+1)), nil
		}
		ans, err := env.AskUser(q.Question, q.Options)
		if err != nil {
			return errResult("", err.Error()), nil
		}
		if ans == "" {
			ans = "(dismissed — no pick; proceed on your best judgment)"
		}
		fmt.Fprintf(&b, "Q: %s\nA: %s\n", q.Question, ans)
	}
	return okText("", strings.TrimRight(b.String(), "\n")), nil
}
