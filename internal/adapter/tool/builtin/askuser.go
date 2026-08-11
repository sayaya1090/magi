package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/report"
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
	// Report is the grounds the person decides on, one entry per section the decision-report skill
	// declares. A map because the SECTIONS are not fixed by this file — the operator's skill names
	// them, and a struct here would freeze somebody else's taste into the tool.
	Report map[string]string `json:"report"`
}

type askUserArgs struct {
	Questions []askUserQ `json:"questions"`
}

func (AskUser) Name() string { return "ask_user" }
func (AskUser) Description() string {
	return "Ask the USER to choose between concrete options (a selection modal, one question at a time). Use " +
		"when a decision is genuinely the user's — approach, scope, naming, destructive vs safe — with 2-4 real " +
		"alternatives (short labels). Not for decisions with an obvious default, or permission the tool system " +
		"handles. Each question also needs {report}: the grounds the person decides ON, one entry per section " +
		"the decision-report skill asks for — the call is refused with the section list if any is missing, so " +
		"you never have to guess the shape. Each answer is the chosen option's text; empty = dismissed, so " +
		"proceed on your best judgment and say so. Act on the answers directly; if asking was the whole " +
		"request, restate the pick and finish."
}
func (AskUser) Schema() json.RawMessage {
	// report is an open object rather than named properties: which sections exist is read from the
	// decision-report skill at call time, and a schema written here would either contradict that
	// skill or freeze it.
	return json.RawMessage(`{"type":"object","properties":{"questions":{"type":"array","items":{"type":"object",` +
		`"properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},` +
		`"report":{"type":"object","additionalProperties":{"type":"string"}}},` +
		`"required":["question","options","report"]},"minItems":1,"maxItems":4}},"required":["questions"]}`)
}

// contractFor reads the report's shape from the decision-report skill, falling back to the default.
//
// Read per call rather than cached: skills are edited while magi runs, and a companion that picked
// up its contract at startup would keep asking for sections its operator had removed an hour ago.
func contractFor(env port.ToolEnv) report.Contract {
	if env.LoadSkill == nil {
		return report.Default
	}
	body, ok := env.LoadSkill(report.SkillName)
	if !ok {
		return report.Default
	}
	if c := report.Parse(body); len(c) > 0 {
		return c
	}
	// The skill exists and declares no sections. That is a skill somebody is still writing, not an
	// instruction that a decision needs no grounds.
	return report.Default
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
	contract := contractFor(env)
	var b strings.Builder
	for i, q := range a.Questions {
		if strings.TrimSpace(q.Question) == "" || len(q.Options) < 2 {
			return errResult("", fmt.Sprintf("ask_user: question %d needs text and at least 2 options", i+1)), nil
		}
		// Refused, not filled in on the model's behalf. A person is about to decide something on
		// these grounds, and grounds magi invented would be the worst possible thing to put in
		// front of them. The rejection carries the whole contract so one refusal teaches it.
		if missing := contract.Missing(q.Report); len(missing) > 0 {
			return errResult("", fmt.Sprintf(
				"ask_user: question %d has no %s in its report. The person decides on what you write "+
					"here, so every section is required:%s",
				i+1, strings.Join(missing, " and no "), contract.Spec())), nil
		}
		// Which of how many. A call may ask several and each one blocks, so the person answering
		// the first is entitled to know that two more are coming — "is this the whole decision" is
		// part of the decision.
		ans, err := env.AskUser(port.Question{
			Text: q.Question, Options: q.Options, Grounds: contract.Fill(q.Report),
			Index: i + 1, Total: len(a.Questions)})
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
