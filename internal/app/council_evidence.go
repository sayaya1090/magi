package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
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
	case "grep", "glob", "astgrep":
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

// stuckObstacleWords flag a tool result that hit a CONCRETE wall (vs a clean success), so a
// stuck-recovery reason can name the exact obstacle instead of a generic "reasoned in circles".
var stuckObstacleWords = []string{
	"timed out", "no such file", "not found", "cannot ", "permission denied",
	"undefined reference", "fatal", "segmentation fault", "traceback", "assertion",
	"command not found", "failed", "does not exist", "unable to",
}

// stuckEvidence extracts the CONCRETE obstacles this turn's tool calls hit — errored results,
// timeouts, missing files, build failures — so a stuck-recovery's block reason can name the specific
// walls the previous attempt ran into ("address THESE") rather than a generic label the planner can't
// act on. It is the leadership move: a subordinate who is stuck needs the concrete reality and what to
// do differently, not "you went in circles". Deterministic (no LLM call), grounded in real results,
// most-recent obstacles first (capped). Empty when nothing notable failed.
func stuckEvidence(evs []event.Event, k int) string {
	names := map[string]string{}
	var obst []string
	seen := map[string]bool{}
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser { // new turn → keep only the latest turn's obstacles
			names, obst, seen = map[string]string{}, nil, map[string]bool{}
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
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			r := d.Part.ToolResult
			if r == nil {
				continue
			}
			content := strings.TrimSpace(toolResultText(r.Content))
			low := strings.ToLower(content)
			hit := r.IsError
			if !hit {
				for _, w := range stuckObstacleWords {
					if strings.Contains(low, w) {
						hit = true
						break
					}
				}
			}
			if !hit || content == "" {
				continue
			}
			name := names[r.CallID]
			if name == "" {
				name = "tool"
			}
			line := name + ": " + clipLine(content, 160)
			if seen[line] {
				continue
			}
			seen[line] = true
			obst = append(obst, line)
		}
	}
	if len(obst) == 0 {
		return ""
	}
	if len(obst) > k {
		obst = append([]string{fmt.Sprintf("…%d earlier obstacles are not shown", len(obst)-k)},
			obst[len(obst)-k:]...)
	}
	return " Concrete walls the previous attempt hit (address THESE directly — do not just re-analyze or repeat the same commands): " + strings.Join(obst, "; ")
}

// knowledgeLookupTools are the tools whose whole job is to fetch an EXTERNAL FACT the
// agent does not already possess. A failure here that the agent does not recover from
// is the N14 "research dead-end" fabrication branch: the agent fills the gap with a
// guessed premise (e.g. a restriction-enzyme site, an API detail, a constant) and
// proceeds. The execution-evidence gate (runGuard.unverifiedDeliverable, structural,
// about the deliverable existing/being exercised) is blind to it because execution
// succeeds — the lie is in a FACT, not the artifact — and an LLM council cannot verify
// a domain fact from reasoning alone.
var knowledgeLookupTools = map[string]bool{
	"websearch":  true,
	"web_search": true,
	"webfetch":   true,
	"web_fetch":  true,
	"fetch":      true,
}

// unverifiedLookup scans the LATEST turn and returns a non-empty detail when a
// knowledge-lookup tool failed and NO lookup in the turn succeeded — i.e. the agent may
// have proceeded on an unverified external premise. It returns "" when there was no
// failed lookup, or when any lookup succeeded (the agent plausibly recovered a fact).
// Recovery is judged turn-wide, not per-fact: a single successful lookup silences the
// signal even if it answered a different question — a deliberate bias toward silence to
// keep the signal from churning; it under-fires rather than over-fires.
//
// It is deliberately structural and language-agnostic, mirroring
// runGuard.unverifiedDeliverable, and — crucially — it resurfaces a failure that
// turnToolEvidence's most-recent-k window would otherwise age out (the failed lookup
// happens early; the deliverable's format checks happen last), so the council would
// never see the un-verified premise without this signal. Advisory, not a veto: it makes
// the council look harder, exactly like the self-check "unverified" fabrication signal.
func unverifiedLookup(evs []event.Event) string {
	names := map[string]string{} // callID -> tool name
	var failed []string          // "tool: err snippet" for each un-recovered failed lookup
	anySuccess := false          // a lookup returned without error → plausible recovery
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser { // new turn boundary → judge only the latest turn
			names = map[string]string{}
			failed = nil
			anySuccess = false
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
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			r := d.Part.ToolResult
			if r == nil || !knowledgeLookupTools[names[r.CallID]] {
				continue
			}
			if r.IsError {
				failed = append(failed, names[r.CallID]+": "+clipLine(toolResultText(r.Content), councilActionCap))
			} else {
				anySuccess = true
			}
		}
	}
	if len(failed) == 0 || anySuccess {
		return ""
	}
	return "a knowledge lookup failed this turn and no lookup succeeded — any external fact the agent " +
		"went on to use (an API detail, constant, sequence, name, spec) may be an UNVERIFIED guess rather than a " +
		"confirmed value. If the deliverable depends on such a fact, its correctness is unproven:\n- " +
		strings.Join(failed, "\n- ")
}

// lookupRecovered reports whether a knowledge lookup SUCCEEDED in the latest turn — the
// only POSITIVE evidence that an unverified-premise concern is actually resolved. It is
// deliberately distinct from "unverifiedLookup returned empty": empty also covers a turn
// with no lookup at all, and mere absence must NEVER auto-resolve a still-open concern
// (that would let a quiet turn launder away a premise that was never verified). Only a
// real, successful lookup clears it.
func lookupRecovered(evs []event.Event) bool {
	names := map[string]string{}
	recovered := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser { // judge only the latest turn
			names = map[string]string{}
			recovered = false
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
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			r := d.Part.ToolResult
			if r != nil && knowledgeLookupTools[names[r.CallID]] && !r.IsError {
				recovered = true
			}
		}
	}
	return recovered
}

// normEq reports whether two answers are the same modulo whitespace — the
// cheap, deterministic notion of "the agent resubmitted its rejected answer".
func normEq(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// clipLine returns at most n bytes of s (rune-safe) with an ellipsis, keeping a single
// evidence bullet on one line (no marker/newline reintroduced).
func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// clipTail is clipLine for an APPEND-ORDERED list, where the last entry is the newest and the
// first is the most stale. clipLine keeps the head, so in such a list it drops exactly the entry
// the reader most needs — the step that just ran. Keep the tail instead, marking the cut at the
// front so the reader knows earlier entries existed rather than reading a mid-sentence start as
// the beginning of the record.
func clipTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…(earlier entries omitted)\n" + s[cut:]
}

// clipSpec bounds an authoritative "follow VERBATIM" spec at n bytes (rune-safe).
// Unlike clipLine it does NOT append a bare "…": a delegate told to reproduce exact
// identifiers can otherwise copy the dangling ellipsis into an edit old-string (or an
// output the grader checks), matching nothing. When it truncates it appends an explicit
// marker on its own line so the model knows the cutoff is not part of the spec.
func clipSpec(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[…spec truncated here — this cutoff is NOT part of the spec; if you need an exact value beyond this point, ask for the remainder rather than reproducing this line]"
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
func truncateForCouncil(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[diff truncated]"
}

// tailForCouncil keeps at most the last n bytes of s (on a rune boundary), since a
// failing build/test puts the useful output last.
func tailForCouncil(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…[earlier output truncated]\n" + s[cut:]
}

// countToolCalls counts the tool-call parts in the event log — a cheap monotonic
// fingerprint of "did the agent DO anything": equal counts across a rejection →
// re-finish mean zero new actions, so no evidence-based verdict can have changed.
func countToolCalls(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolCall {
			n++
		}
	}
	return n
}

// deltaToolEvidence renders the tool results in evs (no prompt-boundary resets —
// the window IS the delta since the last rejection, which starts right after the
// feedback injection). Format mirrors turnToolEvidence.
func deltaToolEvidence(evs []event.Event, k int) string {
	names := map[string]toolCallBrief{}
	var lines []string
	for _, e := range evs {
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
		if json.Unmarshal(e.Data, &v) != nil || v.Phase != "" {
			continue // a plan-audit verdict judged a plan, not this deliverable
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
