package builtin

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// SearchSessions finds work from earlier days in this workspace.
//
// There are now three ways to remember something, and a model holding all three needs the lines
// between them drawn sharply or it will reach for the wrong one and conclude the thing is gone:
//
//	recall_context  — THIS session, the detail that compaction summarised away
//	recall_memory   — the curated store, what somebody chose to write down with `remember`
//	search_sessions — other days, raw, whatever was actually said
//
// The last is the only one that can find something nobody thought to save at the time, which is
// most of what turns out to matter later.
type SearchSessions struct{}

type searchSessionsArgs struct {
	Query string `json:"query"`
	Open  string `json:"open"`
}

func (SearchSessions) Name() string { return "search_sessions" }

func (SearchSessions) Description() string {
	return "Search this workspace's earlier sessions by keyword — conversations from other days, " +
		"as they happened. Pass {query} with words that would have appeared: a file path, an error " +
		"string, a library name. You get back matching sessions with a line of each matching turn " +
		"and an id like s_abc123#42; pass that as {open} to read the whole turn verbatim. " +
		"Use it when something was decided, tried, or ruled out before and you need what was " +
		"actually said. (Different from recall_context, which restores this session's own " +
		"compacted-out detail, and from recall_memory, which reads only what was deliberately saved.)"
}

func (SearchSessions) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"query":{"type":"string","description":"keywords that would have appeared in the earlier work"},` +
		`"open":{"type":"string","description":"a turn id from a previous search (\"s_abc123#42\") to read that turn whole"}` +
		`}}`)
}

func (SearchSessions) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.SearchSessions == nil {
		return errResult("", "searching earlier sessions is not available here"), nil
	}
	var a searchSessionsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.Query == "" && a.Open == "" {
		return errResult("", "provide a {query} to search for, or an {open} turn id from an earlier search"), nil
	}
	out, err := env.SearchSessions(a.Query, a.Open)
	if err != nil {
		return errResult("", "search failed: "+err.Error()), nil
	}
	return okText("", out), nil
}
