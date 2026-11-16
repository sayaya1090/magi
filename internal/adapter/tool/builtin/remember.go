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
	// Page routes the text to the shared WIKI instead of the append-only memories: the named
	// canonical page is created or updated IN PLACE (default tier: team). Wiki pages are for
	// structure knowledge the other companions need current — how a system works, what owns
	// what — where a correction must replace the old claim, not pile up beside it.
	Page    string `json:"page"`
	Summary string `json:"summary"`
	// Stale, with Page, retires the page: the text becomes the tombstone's body — WHY it stopped
	// being true. The page leaves the index, is demoted in search, and stays readable.
	Stale FlexBool `json:"stale"`
}

func (Remember) Name() string { return "remember" }
func (Remember) Description() string {
	return "Save something worth keeping: concise 'text', optional 'tags'. 'scope' picks where and for how long:\n  \"turn\" — something to check before you declare this task finished; magi hands it back word for word at the finish. Write it as soon as you notice it: a part of the request this step does not touch, something you deferred or stubbed, or a fact that cost real work (a measured value, a proven cause, a ruled-out dead end) — a long task is compacted and may lose it. Not a log of what you did.\n  \"project\" (default) — a durable workspace learning, recallable later via recall_memory.\n  \"team\" — what companions doing related work all need (a team decision, a shared convention, a fact about the system they share); read by every companion on this team and nowhere else.\n  \"global\" — knowledge useful across all projects.\n'page' routes the text to the shared WIKI instead: that canonical page is created or replaced IN PLACE (team-wide by default). Use it for structure knowledge others need current — how a system works, what owns what, where a thing lives — where a correction must replace the old claim. text is the page's new FULL body; update the existing page for a topic rather than minting a near-duplicate title. 'summary' (with page) is one line on what changed and why.\nDo not include secrets."
}
func (Remember) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"tags":{"type":"array","items":{"type":"string"},"description":"labels for a memory; with page, these are the page's LINKS (related page titles)"},"scope":{"type":"string","enum":["turn","project","team","global"],"description":"turn (reminded before this turn ends), project (default), team, or global"},"page":{"type":"string","description":"wiki page title — routes text to the shared wiki as this page's new full body, updated in place"},"summary":{"type":"string","description":"with page: one line on what changed and why"},"stale":{"type":"boolean","description":"with page: retire the page — text becomes the recorded reason it stopped being true"}},"required":["text"]}`)
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
	if scope != "" && scope != "turn" && scope != "project" && scope != "team" && scope != "global" {
		return errResult("", "scope must be \"turn\", \"project\", \"team\" or \"global\""), nil
	}
	// A turn note never leaves this session: it is handed straight back to the agent that wrote
	// it, before the turn can end. Nothing reads it in between. A PAGE with scope "turn" is a
	// contradiction — one asks to be forgotten at the finish, the other to outlive every session
	// — and the old behavior silently kept the note and dropped the page, answering "noted" about
	// a wiki write that never happened.
	if scope == "turn" {
		if strings.TrimSpace(a.Page) != "" {
			return errResult("", "a wiki page cannot have scope \"turn\" — a page outlives the turn by definition; drop the scope (team is the default) or drop the page"), nil
		}
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
	// A page write goes to the wiki: the named canonical page, updated in place, team tier by
	// default. The text is the page's NEW full body — a snapshot, because the page IS the current
	// truth about its topic and a reader must never have to assemble it from fragments.
	if page := strings.TrimSpace(a.Page); page != "" {
		// Asked BEFORE the write, so the just-written page cannot be its own neighbor. The note
		// rides the success text: convergence pressure against near-duplicate titles, never a
		// refusal — the write is still wanted; what the note buys is the NEXT write landing on
		// the existing title.
		advise := ""
		if env.WikiAdvise != nil {
			advise = env.WikiAdvise(page)
		}
		// Source left empty on purpose: the app stamps it with this companion's declared name, so
		// the page's editor line says WHO in the fleet wrote it — the one fact a reader of a
		// shared page wants that a bare "agent" cannot carry.
		if err := env.Propose(port.Contribution{
			Wiki:  []port.WikiEdit{{Page: page, Text: a.Text, Links: a.Tags, Summary: a.Summary, Stale: bool(a.Stale)}},
			Scope: scope,
		}); err != nil {
			return errResult("", err.Error()), nil
		}
		msg := "wiki page " + page + " updated — the other companions see it via recall_memory and the wiki index"
		if a.Stale {
			msg = "wiki page " + page + " marked STALE — it leaves the index, stays readable, and your text is the recorded reason"
		}
		if advise != "" {
			msg += "\n" + advise
		}
		return okText("", msg), nil
	}
	if err := env.Propose(port.Contribution{
		Memories: []port.Memory{{Text: a.Text, Tags: a.Tags}},
		Source:   "agent",
		Scope:    scope,
	}); err != nil {
		return errResult("", err.Error()), nil
	}
	where := "project"
	if scope == "team" || scope == "global" {
		where = scope
	}
	return okText("", "saved to "+where+" memory (recallable via recall_memory)"), nil
}
