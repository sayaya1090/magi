package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/rank"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Finding a thing that was said on another day.
//
// # The gap this fills
//
// Everything a companion has ever done in a workspace is on disk, in append-only logs, and until
// now there was no way to read any of it back except by remembering which session it was in and
// resuming that. Recall reached two places — the compacted-out detail of the CURRENT session
// (recall_context) and the curated store that `remember` writes (recall_memory) — and neither
// reaches last Tuesday's raw conversation.
//
// # Why a turn is the document, and not a session
//
// This is the one decision here that is not obvious, and getting it wrong makes the feature
// useless rather than merely worse.
//
// rank.ByIDF scores a document by which of the query's words appear in it. It has no term
// frequency and no length normalisation — presence, weighted by rarity. Feed it whole sessions and
// the longest session in the workspace contains every word in every query, so it wins all of them.
// The biggest log in this repository is tens of megabytes; it would be the answer to everything.
//
// A turn — one user prompt and what the companion did about it — is roughly uniform in size, and it
// is also the thing a person is actually looking for. So turns are the documents, and the hits are
// grouped back into sessions for display.
//
// # Lexical only, and it says so
//
// No embeddings. The companion store that fuses lexical with semantic ranking holds a few dozen
// notes; this holds thousands of turns, and embedding all of them is a cost of a different order.
// The answer says "by wording" for the same reason that one does: a search that quietly became
// worse is a search whose results nobody can interpret.

// searchLimit is how many sessions a search names before it stops and says how many it left.
const searchLimit = 8

// snippetsPerSession bounds how much of one session's match is shown. A session where every turn
// mentions the query would otherwise fill the answer by itself.
const snippetsPerSession = 3

// turnDoc is one turn, flattened for searching.
type turnDoc struct {
	Session session.SessionID
	Seq     int64 // the prompt event's seq — the turn's address, for reading it back whole
	At      time.Time
	Prompt  string // what was asked, for the snippet
	Text    string // what is searched: the prompt, the replies, and the names of the tools used
}

// cachedTurns is one session's turns and the activity timestamp they were built from.
type cachedTurns struct {
	at    time.Time
	turns []turnDoc
}

// SearchSessions ranks past turns in this workspace against a query and answers in lines.
//
// Lines rather than JSON, and grouped by session, because the reader is deciding which day to go
// back to — that decision is made on when it was, what the session was called, and a few words of
// the turn, not on a schema.
func (a *App) SearchSessions(ctx context.Context, workdir, query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "Say what to look for.", nil
	}
	metas, err := a.store.ListSessions(ctx, workdir)
	if err != nil {
		return "", err
	}
	var docs []turnDoc
	for _, m := range metas {
		turns, terr := a.turnsOf(ctx, m)
		if terr != nil {
			continue // one unreadable log should not take the search down with it
		}
		docs = append(docs, turns...)
	}
	if len(docs) == 0 {
		return "There is no earlier work in this workspace to search.", nil
	}

	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Text
	}
	hits := rank.ByIDF(query, texts)
	if len(hits) == 0 {
		// Named counts, so "nothing on this" can be told apart from "the search is broken".
		return fmt.Sprintf("Nothing in %d turns across %d sessions mentions %q.",
			len(docs), len(metas), query), nil
	}

	// Group by session, keeping each session's best rank and its best few turns.
	type group struct {
		meta  session.SessionMeta
		best  float64
		turns []turnDoc
		total int
	}
	byID := map[session.SessionID]*group{}
	metaOf := map[session.SessionID]session.SessionMeta{}
	for _, m := range metas {
		metaOf[m.ID] = m
	}
	var order []*group
	for _, h := range hits {
		d := docs[h.Index]
		g := byID[d.Session]
		if g == nil {
			g = &group{meta: metaOf[d.Session], best: h.Score}
			byID[d.Session] = g
			order = append(order, g)
		}
		g.total++
		if len(g.turns) < snippetsPerSession {
			g.turns = append(g.turns, d)
		}
	}
	// hits is already best-first and groups were appended in that order, so order is by best turn.
	// Made explicit rather than relied on, because "it happens to come out sorted" is how a sort
	// stops being true.
	sort.SliceStable(order, func(i, j int) bool { return order[i].best > order[j].best })

	var b strings.Builder
	shown := min(len(order), searchLimit)
	fmt.Fprintf(&b, "%q — %d of %d sessions, by wording, best first. "+
		"Read one whole with open (the id after each turn).\n", query, shown, len(metas))
	for i, g := range order {
		if i == searchLimit {
			fmt.Fprintf(&b, "\n(+%d more sessions — narrow the words)\n", len(order)-searchLimit)
			break
		}
		title := g.meta.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "\n%s · %s · %s", g.meta.Created.Format("2006-01-02"), g.meta.ID, title)
		if origin, ok := CronOriginName(g.meta.Origin); ok {
			// Worth saying: unattended work reads very differently from something a person asked for.
			fmt.Fprintf(&b, " · scheduled (%s)", origin)
		}
		if g.total > len(g.turns) {
			fmt.Fprintf(&b, " · %d matching turns", g.total)
		}
		for _, d := range g.turns {
			fmt.Fprintf(&b, "\n    %s#%d  %s", d.Session, d.Seq, clipLine(oneLine(d.Prompt), 100))
		}
	}
	b.WriteString("\n")
	return b.String(), nil
}

// OpenTurn returns one turn whole, verbatim, addressed as it was printed: "<session>#<seq>".
func (a *App) OpenTurn(ctx context.Context, ref string) (string, error) {
	sidText, seqText, ok := strings.Cut(strings.TrimSpace(ref), "#")
	if !ok {
		return fmt.Sprintf("%q is not a turn id. They look like s_abc123#42, and the search prints one after every match.", ref), nil
	}
	var seq int64
	if _, err := fmt.Sscanf(seqText, "%d", &seq); err != nil {
		return fmt.Sprintf("%q is not a turn id: %q is not a number.", ref, seqText), nil
	}
	sid := session.SessionID(sidText)
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return fmt.Sprintf("Cannot read session %s: %v", sid, err), nil
	}
	if len(evs) == 0 {
		return fmt.Sprintf("Session %s has nothing in it.", sid), nil
	}
	start := -1
	for i, e := range evs {
		if e.Seq == seq && e.Type == event.TypePromptSubmitted {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Sprintf("Session %s has no turn at %d.", sid, seq), nil
	}
	end := len(evs)
	for i := start + 1; i < len(evs); i++ {
		if isTurnStart(evs[i]) {
			end = i
			break
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s#%d — %s]\n", sid, seq, evs[start].TS.Format(time.RFC3339))
	for _, e := range evs[start:end] {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				for _, p := range d.Parts {
					if t := strings.TrimSpace(p.Text); t != "" {
						b.WriteString("\nuser: " + t)
					}
				}
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if line := renderRecalledMessage(session.Message{Role: d.Role, Parts: []session.Part{d.Part}}); strings.TrimSpace(line) != "" {
				b.WriteString("\n" + line)
			}
		}
	}
	b.WriteString("\n")
	return capRecall(b.String()), nil
}

// capRecall keeps one turn from refilling the window it was read out of. Same ceiling recall_context
// uses, and the same reason.
func capRecall(s string) string {
	if len(s) <= recallRenderCap {
		return s
	}
	return s[:recallRenderCap] + "\n[…trimmed]"
}

// turnsOf returns a session's turns, rebuilding them only when the session has moved.
//
// Keyed on LastActivity rather than on a file's mtime: the logs are append-only and the store is
// reached through its interface, so "has this session changed" is a question the metadata already
// answers and no filesystem detail needs to leak in here.
func (a *App) turnsOf(ctx context.Context, m session.SessionMeta) ([]turnDoc, error) {
	a.searchMu.Lock()
	if c, ok := a.searchCache[m.ID]; ok && c.at.Equal(m.LastActivity) {
		a.searchMu.Unlock()
		return c.turns, nil
	}
	a.searchMu.Unlock()

	evs, err := a.store.Read(ctx, m.ID, 0)
	if err != nil {
		return nil, err
	}
	turns := buildTurns(m.ID, evs)

	a.searchMu.Lock()
	if a.searchCache == nil {
		a.searchCache = map[session.SessionID]cachedTurns{}
	}
	a.searchCache[m.ID] = cachedTurns{at: m.LastActivity, turns: turns}
	a.searchMu.Unlock()
	return turns, nil
}

// isTurnStart reports whether an event begins a new top-level turn. Only a user prompt does —
// magi's own injected prompts carry a system actor and are part of the turn they were injected
// into, not the start of another one.
func isTurnStart(e event.Event) bool {
	return e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser
}

// buildTurns slices a session's events into turns and flattens each into searchable text.
func buildTurns(sid session.SessionID, evs []event.Event) []turnDoc {
	var out []turnDoc
	var cur *turnDoc
	var text strings.Builder

	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = text.String()
		out = append(out, *cur)
		cur = nil
		text.Reset()
	}

	for _, e := range evs {
		if isTurnStart(e) {
			flush()
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			var asked []string
			for _, p := range d.Parts {
				if p.Kind == session.PartText {
					if t := strings.TrimSpace(p.Text); t != "" {
						asked = append(asked, t)
					}
				}
			}
			prompt := strings.Join(asked, " ")
			cur = &turnDoc{Session: sid, Seq: e.Seq, At: e.TS, Prompt: prompt}
			text.WriteString(prompt)
			continue
		}
		if cur == nil || e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch d.Part.Kind {
		case session.PartText:
			// What the companion said. Reasoning is left out: it is the largest thing in a log and
			// the least like what somebody would search for.
			text.WriteString(" " + d.Part.Text)
		case session.PartToolCall:
			// Names only, not arguments and not results. Results are most of a log's bytes and
			// almost all of its noise — a build's whole stdout would make every session match
			// "error". The name is the part a person remembers ("the run where it used ripgrep").
			if d.Part.ToolCall != nil {
				text.WriteString(" " + d.Part.ToolCall.Name)
			}
		}
	}
	flush()
	return out
}

// oneLine collapses a prompt's whitespace so a multi-line ask fits on a snippet row. The cutting
// is clipLine's job (council_evidence.go), which is already the rune-safe one.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
