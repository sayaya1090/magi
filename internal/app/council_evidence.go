package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"

	"github.com/sayaya1090/magi/internal/core/text"
)

// toolCallBrief is what a finished call needs to be legible as evidence: the tool's name and the
// request that produced the result.
type toolCallBrief struct{ name, args string }

// evidenceLine renders one finished call for a council block: what was ASKED, then what came back.
//
// It used to render only the answer — `tool bash [ok]: <output>` — and a result without its request
// is half a fact. Observed live (sqlite-with-gcov, 2026-07-30): the agent ran
// `ln -sf /app/sqlite/sqlite3 /usr/local/bin/sqlite3 && which sqlite3 && sqlite3 --version`, whose
// output is `/usr/local/bin/sqlite3` and a version string — byte-for-byte the same answer a bare
// `which sqlite3 && sqlite3 --version` gives. With the command hidden, a member concluded and told
// the agent that "the sqlite3 command works only because /usr/local/bin/sqlite3 exists from before
// this task", which was false: the agent had created that symlink two and a half minutes earlier,
// and the call that did it was in the block. The evidence was there; only the half that identified
// it was missing.
//
// The request is clipped hard. It is there to say WHICH call this was, not to reproduce it.
func evidenceLine(b toolCallBrief, status, result string) string {
	head := "tool " + b.name + " [" + status + "]"
	if req := strings.TrimSpace(clipLine(b.args, evidenceArgsCap)); req != "" {
		head += " " + req
	}
	// The read tool prefixes every line with "N⇥" (a line-number gutter), and the flattener turns
	// that into "1\t1 ⏎ 2\t2 ⏎ …" — which three members read as "the file has duplicated numbers"
	// and rejected a byte-perfect file over. Say once that the leading number is the gutter, not
	// the content.
	if b.name == "read" {
		head += " [each line is prefixed 'N⇥' by the read tool — that leading number is a line number, not file content]"
	}
	return head + ": " + clipLine(result, councilActionCap)
}

// evidenceArgsCap bounds the request shown per line. Long enough that a build command, a path, or a
// redirect target survives whole; short enough that eight of them cost nothing beside the results.
const evidenceArgsCap = 300

// evidenceArgs picks the part of a call's arguments that identifies it, which depends on the tool.
// bash carries no path, so its command IS its identity; a file tool is named by what it pointed at;
// a SEARCH is named by what it looked for — two greps of different patterns under one directory are
// different calls, and the directory alone would render them the same. Empty when the arguments
// carry nothing identifying (a bare `council{complete:true}`), which leaves the line as it was.
func evidenceArgs(name string, raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	str := func(k string) string {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	switch name {
	case "grep", "glob":
		// Both halves, because either alone is ambiguous across a turn's searches.
		what := str("pattern")
		if what == "" {
			what = str("query")
		}
		where := str("path")
		switch {
		case what != "" && where != "":
			return what + " in " + where
		case what != "":
			return what
		default:
			return where
		}
	}
	for _, k := range []string{"command", "path", "file", "pattern", "query", "url", "id"} {
		if s := str(k); s != "" {
			return s
		}
	}
	return ""
}

// turnToolEvidence summarizes THIS turn's tool RESULTS as real, git-independent
// evidence of what actually happened — a write that reported bytes, a `cat` that shows
// the content. It deliberately EXCLUDES the model's own text: that is the agent's claim
// (already passed as Report), and admitting narration as "evidence" is exactly how a
// defeatist agent talks the council into "done" with no artifact (the download-youtube
// lesson). Only events since the last user prompt are considered, so a prior turn's
// successful tool result can't masquerade as this turn's. Most recent k results.
func turnToolEvidence(evs []event.Event, k int) string {
	names := map[string]toolCallBrief{} // callID -> what was asked
	var lines []string
	for _, e := range evs {
		// A turn begins when the USER speaks. magi injects prompt.submitted events of its own —
		// the stall nudge, a plugin note, a permission message, a hook, an orchestrator re-prompt
		// — and treating those as boundaries emptied the very block the council judges on.
		// Measured: a turn whose three results included two failing `cleanup ran!` lines came back
		// as ONE line after a nudge, and as NOTHING when the council was convened right after one.
		// lastUserPromptTS below has always asked for ActorUser; these scans had drifted.
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			names = map[string]toolCallBrief{}
			lines = nil
			continue
		}
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch d.Part.Kind {
		case session.PartToolCall:
			if d.Part.ToolCall != nil {
				names[d.Part.ToolCall.CallID] = toolCallBrief{
					name: d.Part.ToolCall.Name,
					args: evidenceArgs(d.Part.ToolCall.Name, d.Part.ToolCall.Args),
				}
			}
		case session.PartToolResult:
			if d.Part.ToolResult == nil {
				continue
			}
			b := names[d.Part.ToolResult.CallID]
			if b.name == "" {
				b.name = "tool"
			}
			status := "ok"
			if d.Part.ToolResult.IsError {
				status = "error"
			}
			lines = append(lines, evidenceLine(b, status, toolResultText(d.Part.ToolResult.Content)))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	// Say how many were left out. The block reads as "this turn's evidence" and is a TAIL: a turn
	// with forty results hands the council the last eight, and the failing one from early on is
	// simply not there. Nothing said so, so a reader had no way to know it was looking at part of
	// the record — the harm priorCouncilObjections below documents from a run that failed, where
	// each deliberation could see only the round before it. clipEach in this same file has always
	// marked its drop; these did not.
	if len(lines) > k {
		dropped := len(lines) - k
		lines = append([]string{fmt.Sprintf("…%d earlier tool results this turn are not shown", dropped)},
			lines[len(lines)-k:]...)
	}
	return "- " + strings.Join(lines, "\n- ")
}

// lastUserPromptTS returns the timestamp of the most recent GENUINE user prompt in evs
// (the turn boundary). Injected subagent results and escalations are ActorAgent prompts,
// so they are skipped — only an ActorUser prompt starts a top-level turn. Zero when none.
func lastUserPromptTS(evs []event.Event) time.Time {
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			return e.TS
		}
	}
	return time.Time{}
}

// clipLine returns at most n bytes of s (rune-safe) with an ellipsis, keeping a single
// evidence bullet on one line (no marker/newline reintroduced).
func clipLine(s string, n int) string { return text.Clip(s, n) }

// clipSpec bounds an authoritative "follow VERBATIM" spec at n bytes (rune-safe).
// Unlike clipLine it does NOT append a bare "…": a delegate told to reproduce exact
// identifiers can otherwise copy the dangling ellipsis into an edit old-string (or an
// output the grader checks), matching nothing. When it truncates it appends an explicit
// marker on its own line so the model knows the cutoff is not part of the spec.
func clipSpec(s string, n int) string {
	return text.ClipWith(s, n, "\n[…spec truncated here — this cutoff is NOT part of the spec; if you need an exact value beyond this point, ask for the remainder rather than reproducing this line]")
}

// toolResultText renders a tool result's JSON content as readable one-ish-line text
// (unwrapping a JSON string, collapsing newlines) for the council evidence summary.
func toolResultText(raw json.RawMessage) string {
	s := string(raw)
	var str string
	if json.Unmarshal(raw, &str) == nil {
		s = str
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " ⏎ "))
}

// truncateForCouncil clips s to at most n bytes (on a rune boundary), appending a
// marker when truncated.
func truncateForCouncil(s string, n int) string { return text.ClipWith(s, n, "\n…[diff truncated]") }

// clipEach returns at most n entries, with a marker when more were dropped.
func clipEach(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), fmt.Sprintf("…and %d more", len(xs)-n))
}

// priorCouncilObjections returns, verbatim, what THIS council already told the agent this turn and
// did not accept — most recent first, one line per member per round.
//
// The council's own words used to reach it only the way any other tool result did: through
// turnToolEvidence, which keeps the most recent councilActionsCap results and drops the rest.
// Measured on a run that failed: five deliberations, and each one could see at most the single
// round immediately before it — c2 raised the exact defect the verifier later failed on (a task
// cancelled while waiting on the semaphore never reaches its `finally`), and c3, c4 and c5 never
// saw that sentence again. c5 accepted on evidence that never exercised the case, and the graded
// test found 0 cleanups where it required 2.
//
// So this reads the FACTS magi recorded rather than the transcript it clips: council.verdict
// carries every member's feedback, and it is a first-class fact precisely because it is worth more
// than one line of scrollback. It states what was said and nothing about whether it was answered —
// magi cannot know that, and a member told "this was ignored" would be judging an accusation
// instead of the work.
func priorCouncilObjections(evs []event.Event, maxItems, perItemCap int) string {
	var lines []string
	seen := map[string]bool{}
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser { // a new turn — earlier rounds judged other work
			lines, seen = nil, map[string]bool{}
			continue
		}
		if e.Type != event.TypeCouncilVerdict {
			continue
		}
		var v event.CouncilVerdictData
		if json.Unmarshal(e.Data, &v) != nil {
			continue
		}
		fb := strings.TrimSpace(v.Feedback)
		if !strings.EqualFold(v.Decision, "continue") || fb == "" || seen[fb] {
			continue
		}
		seen[fb] = true
		who := v.Member
		if v.Lens != "" {
			who += " (" + v.Lens + ")"
		}
		lines = append(lines, who+": "+clipLine(fb, perItemCap))
	}
	if len(lines) == 0 {
		return ""
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 { // most recent first
		lines[i], lines[j] = lines[j], lines[i]
	}
	if len(lines) > maxItems {
		lines = lines[:maxItems]
	}
	return "── WHAT THIS COUNCIL SAID EARLIER THIS TURN, WHEN IT DID NOT ACCEPT ──\n- " +
		strings.Join(lines, "\n- ")
}

// clipEvidenceForRecord bounds the evidence copy kept in the convened FACT, holding on to both
// ends when it has to cut.
//
// truncateForCouncil, which this replaced here, drops the tail — and the tail of the evidence block
// is its most recent results, which is what a decision turns on. Observed in the field
// (cancel-async-tasks, 2026-07-31): the recorded block ended `bash [error] pyth` and then stopped,
// so the last thing the members were handed was, in the record, a seventeen-character stub. Anyone
// reading it back — including the reader trying to work out why a round voted the way it did —
// sees an earlier result as the final one. Its marker also said "diff truncated" in a block that
// is not a diff.
//
// Head and tail, with the omission stated in bytes, is the shape magi already uses for captured
// command output; the same reasons apply.
func clipEvidenceForRecord(s string, n int) string {
	if len(s) <= n || n <= 0 {
		return s
	}
	const marker = "\n…(%d bytes omitted from the middle of this record; the members were given all of it)…\n"
	head, tail := n/2, n-n/2
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	from := len(s) - tail
	for from < len(s) && !utf8.RuneStart(s[from]) {
		from++
	}
	if from <= head {
		return s
	}
	return s[:head] + fmt.Sprintf(marker, from-head) + s[from:]
}
