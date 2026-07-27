package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A stopped exploration's PROSE is unusable — it is whatever the model happened to be saying when it
// was cut off. Its SEARCHES are a different kind of thing entirely: a grep that matched nothing is
// recorded by magi, from the call's own arguments and its own result, with no model text in the
// path. That is the same provenance rule the check audit uses (read magi's record, not the model's
// claim), and it is why a negative can be salvaged from a reply that must otherwise be thrown away.
//
// Only NEGATIVES are salvaged, and the asymmetry is deliberate. A "this name is not in the tree"
// fact can only stop a later step from building on something that does not exist; it can never make
// one adopt a wrong name. Positives are the opposite — a half-finished exploration's hits are
// exactly the fragments the discard exists to keep out — so they stay discarded.

// exploreNegCap bounds how many searched-and-not-found facts one stopped exploration contributes.
// An explorer that spun through dozens of searches has little to say, and a long list would crowd
// out the planner's own grounding.
const exploreNegCap = 12

// searchMiss is one pattern the explorer searched for and did not find, with the scope it searched
// — a miss under one directory says far less than a miss across the tree, so the scope travels with
// it rather than being flattened into a bare "not found".
type searchMiss struct{ pattern, scope string }

// searchedNotFound reads a child session's tool log and returns the searches that established an
// absence. Best-effort throughout: an unreadable session, a result shape grep does not produce, or
// an errored call yields nothing rather than a guess, because a missing record must never be
// reported as a proven absence.
func (a *App) searchedNotFound(ctx context.Context, sid session.SessionID) []searchMiss {
	if sid == "" {
		return nil
	}
	return searchMissesIn(a.readEventsBestEffort(ctx, sid))
}

// searchMissesIn scans one session's tool log, pairing each grep CALL with ITS OWN result by call
// id. The pairing is the whole point: the pattern lives in the arguments and the outcome in the
// result, and a fact that spans the two cannot be forged by either side alone.
func searchMissesIn(evs []event.Event) []searchMiss {
	type query struct{ pattern, scope string }
	pending := map[string]query{}
	hit := map[string]bool{}  // matched SOMEWHERE in this session — never report it as absent
	seen := map[string]bool{} // one line per pattern, however many times it was searched
	var out []searchMiss
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch {
		case d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil:
			// grep only. A glob's empty answer is about FILENAMES and is far easier to write wrongly
			// (a pattern shape that can never match reads exactly like an empty directory), so it is
			// not evidence of absence; grep walks the tree and reports the lines it read.
			if !strings.EqualFold(strings.TrimSpace(d.Part.ToolCall.Name), "grep") {
				continue
			}
			var g struct{ Pattern, Glob, Path string }
			if json.Unmarshal(d.Part.ToolCall.Args, &g) != nil {
				continue
			}
			if p := strings.TrimSpace(g.Pattern); p != "" {
				pending[d.Part.ToolCall.CallID] = query{pattern: p, scope: searchScope(g.Path, g.Glob)}
			}
		case d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil:
			q, ok := pending[d.Part.ToolResult.CallID]
			if !ok {
				continue
			}
			delete(pending, d.Part.ToolResult.CallID)
			if d.Part.ToolResult.IsError {
				continue // a refused call or an invalid regex establishes nothing about the tree
			}
			var lines []string
			if json.Unmarshal(d.Part.ToolResult.Content, &lines) != nil {
				continue // not the array grep returns — no opinion
			}
			if len(lines) > 0 {
				hit[q.pattern] = true
				continue
			}
			if !seen[q.pattern] {
				seen[q.pattern] = true
				out = append(out, searchMiss{pattern: clipSpec(q.pattern, 120), scope: q.scope})
			}
		}
	}
	// A pattern can miss under a narrow path and then hit across the tree. The hit is the stronger
	// fact and it may arrive after the miss, so the filter runs at the end, not inline.
	kept := out[:0]
	for _, m := range out {
		if hit[m.pattern] || len(kept) >= exploreNegCap {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// searchScope names what a grep actually covered, in the words the note will show.
func searchScope(path, glob string) string {
	p, g := strings.TrimSpace(path), strings.TrimSpace(glob)
	switch {
	case p == "" && g == "":
		return "anywhere in the workspace"
	case p == "":
		return "in any `" + g + "` file in the workspace"
	case g == "":
		return "anywhere under `" + p + "`"
	default:
		return "in any `" + g + "` file under `" + p + "`"
	}
}

// renderSearchMisses turns the salvaged absences into the note a later reader gets. The header has
// to do two things the ordinary findings note does not: say where this came from (magi's record of
// the searches, NOT the explorer's report, which was discarded), and state the one inference it
// licenses — do not build on these names — since a reader that treated it as repository findings
// would have it exactly backwards.
func renderSearchMisses(ms []searchMiss) string {
	if len(ms) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Searched and NOT found — an exploration was stopped before it could report, so its " +
		"findings were discarded; these are magi's own record of the searches it had already run, not its " +
		"prose. Each pattern below was searched for and matched NOTHING. Do not plan a step or write a check " +
		"around one of these names as if it already existed: either the plan must CREATE it, or the real name " +
		"is different and has to be found first (search for it yourself before you depend on it).\n")
	for _, m := range ms {
		b.WriteString("- `" + m.pattern + "` — no match " + m.scope + "\n")
	}
	return strings.TrimSpace(b.String())
}
