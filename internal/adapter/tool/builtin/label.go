package builtin

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// What this piece of work is ABOUT, in the agent's own words.
//
// # Why it is recorded and not derived
//
// Everything else a screen says about a finished session is derived: how long it took, how many
// turns, which model, whether anybody stepped in. None of that answers "what was this" — "the
// billing refactor" is not visible in a count of anything, and it cannot be recovered later from a
// log that never held it.
//
// That is not a hole in "derive, never record". The rule exists so state cannot go stale, and a
// label is not state: it is a judgement made while the work was happening, like a todo or a
// remembered fact. It ages the way the work does.
//
// # Free-form, and why that is not a mistake
//
// A fixed vocabulary would mean somebody maintaining a list, and a list that is wrong on the day a
// new kind of work appears. Free-form risks the opposite — a model inventing a new word each time,
// which is a label that labels nothing.
//
// What makes them converge is being shown what already exists. The console lists the labels in use
// and the tool's own answer names them back, so the next call has the vocabulary in front of it.
// This is the same shape the experience store uses to show a lesson settling: nothing enforces
// convergence, the reader is simply able to see it.
type Label struct{}

func (Label) Name() string { return "label" }

func (Label) Description() string {
	return "Say what this piece of work is about, in a word or two — 'billing', 'flaky-test', " +
		"'perf'. It is recorded with the session and shown on the board, so somebody looking back " +
		"at a week can find this without reading it. Pass the WHOLE set each time; passing an " +
		"empty list clears them. Reuse a label that already exists rather than coining a near-" +
		"synonym: the answer names the ones in use."
}

func (Label) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"labels":{"type":"array","items":{"type":"string"}}},"required":["labels"]}`)
}

// most is the ceiling. Five is more than a piece of work is ever about, and a card that carries
// twelve chips is a card nobody reads — the cap is here rather than in the page because a store
// full of noise cannot be fixed by drawing less of it.
const most = 5

func (Label) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.SetLabels == nil {
		return errResult("", "labels are not available in this context"), nil
	}
	var a struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	// Lowercased, trimmed, deduplicated. "Billing", "billing " and "billing" are one label, and
	// three spellings of it on a board is the failure this tool exists to avoid.
	seen := map[string]bool{}
	clean := make([]string, 0, len(a.Labels))
	for _, l := range a.Labels {
		l = strings.ToLower(strings.Join(strings.Fields(l), " "))
		if l == "" || seen[l] || len(clean) == most {
			continue
		}
		seen[l] = true
		clean = append(clean, l)
	}
	sort.Strings(clean)
	env.SetLabels(clean)
	if len(clean) == 0 {
		return okText("", "labels cleared"), nil
	}
	return okText("", "labelled: "+strings.Join(clean, ", ")), nil
}
