package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
// guidanceRead is the full text of every skill the agent opened this SESSION (the `skill`
// tool), latest reading of each, in first-read order. Skills carry instructions the agent
// bound itself to — a layout rule, a "render each finished page" step — and a council that
// judges only against the task cannot see a skill's rule being skipped. Unlike the tool
// evidence this is not reset at each user prompt: a skill read in turn 1 still binds turn 3.
// Each body is clipped at perCap bytes and the whole at totalCap; skills that do not fit are
// named so the reader knows what it is not seeing.
func guidanceRead(evs []event.Event, perCap, totalCap int) string {
	byCall := map[string]string{} // callID -> skill name
	body := map[string]string{}   // skill name -> latest body
	var order []string
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
			tc := d.Part.ToolCall
			if tc == nil || tc.Name != "skill" {
				continue
			}
			var sa struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(tc.Args, &sa) == nil && sa.Name != "" {
				byCall[tc.CallID] = sa.Name
			}
		case session.PartToolResult:
			tr := d.Part.ToolResult
			if tr == nil || tr.IsError {
				continue
			}
			name, ok := byCall[tr.CallID]
			if !ok {
				continue
			}
			text := strings.TrimSpace(toolResultText(tr.Content))
			if text == "" {
				continue
			}
			if _, seen := body[name]; !seen {
				order = append(order, name)
			}
			body[name] = text
		}
	}
	var b strings.Builder
	var omitted []string
	for _, name := range order {
		t := body[name]
		if len(t) > perCap {
			t = clipLine(t, perCap) + "\n[clipped: skill " + name + " is " + strconv.Itoa(len(body[name])) + " bytes]"
		}
		entry := "## skill " + name + "\n" + t + "\n\n"
		if b.Len()+len(entry) > totalCap {
			omitted = append(omitted, name)
			continue
		}
		b.WriteString(entry)
	}
	if len(omitted) > 0 {
		b.WriteString("[" + strconv.Itoa(len(omitted)) + " more skill(s) the agent read did not fit: " +
			strings.Join(omitted, ", ") + "]\n")
	}
	return strings.TrimSpace(b.String())
}

// evidenceKeptPerTool bounds how many pre-window "last result of tool X" lines are kept, so a turn
// that touched twenty different tools does not smuggle twenty lines past the window.
const evidenceKeptPerTool = 6

func turnToolEvidence(evs []event.Event, k int) string {
	names := map[string]toolCallBrief{} // callID -> what was asked
	// One entry per finished call, keyed so a REPEAT of the same call can supersede the earlier
	// result — see the collapse below the scan.
	type entry struct {
		name string // the tool, so a window can tell what it is about to drop
		key  string // name + identifying args; "" = no identity, never superseded
		line string
		stub string // what the line collapses to when a later result answers the same call
	}
	var ents []entry
	for _, e := range evs {
		// A turn begins when the USER speaks. magi injects prompt.submitted events of its own —
		// the stall nudge, a plugin note, a permission message, a hook, an orchestrator re-prompt
		// — and treating those as boundaries emptied the very block the council judges on.
		// Measured: a turn whose three results included two failing `cleanup ran!` lines came back
		// as ONE line after a nudge, and as NOTHING when the council was convened right after one.
		// lastUserPromptTS below has always asked for ActorUser; these scans had drifted.
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			names = map[string]toolCallBrief{}
			ents = nil
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
			key, stub := "", ""
			if b.args != "" {
				key = b.name + "\x00" + b.args
				stub = "tool " + b.name + " [" + status + ", superseded] " +
					clipLine(b.args, evidenceArgsCap) +
					": this same call ran AGAIN later — its newer result below is the one that " +
					"reflects the current state; this older output is omitted so it cannot be " +
					"mistaken for the file as it is now"
			}
			ents = append(ents, entry{name: b.name, key: key, line: evidenceLine(b, status, toolResultText(d.Part.ToolResult.Content)), stub: stub})
		}
	}
	if len(ents) == 0 {
		return ""
	}
	// Collapse stale repeats. A member handed three reads of one file has no way to know which is
	// current, and the record shows what it chooses instead: the FIRST, biggest snapshot. Observed
	// live (the itm→item rename, 2026-08-16): the work was correct before the first declaration,
	// but the block opened with a mid-rename read of the same file, and all three members cited
	// its long-fixed lines to reject three declarations in a row — while the passing pytest and
	// the current content sat lower in the very same block. Same call asked again = the older
	// answer is stale by definition, so only the newest keeps its output; earlier ones keep their
	// status (an "error then ok" history stays legible) but lose the content a reader could anchor
	// on.
	last := map[string]int{}
	for i, en := range ents {
		if en.key != "" {
			last[en.key] = i
		}
	}
	var lines []string
	for i, en := range ents {
		if en.key != "" && last[en.key] != i {
			lines = append(lines, en.stub)
			continue
		}
		lines = append(lines, en.line)
	}
	// Say how many were left out. The block reads as "this turn's evidence" and is a TAIL: a turn
	// with forty results hands the council the last eight, and the failing one from early on is
	// simply not there. Nothing said so, so a reader had no way to know it was looking at part of
	// the record — the harm priorCouncilObjections below documents from a run that failed, where
	// each deliberation could see only the round before it. clipEach in this same file has always
	// marked its drop; these did not.
	if len(lines) > k {
		// **A window of the last k drops the one result the task hinges on.** Live 2026-09-05
		// (IR deck, second run): read_notes proved the cover disclaimer, then eight render_slide
		// and eight read_slide calls followed, and the council — shown only the last 8 — voted
		// 3:0 "no read_notes evidence". So the latest result of every tool that has none inside
		// the window is kept above it, marked; the window itself is unchanged.
		cut := len(lines) - k
		inWindow := map[string]bool{}
		for _, en := range ents[cut:] {
			inWindow[en.name] = true
		}
		latest := map[string]int{}
		for i := 0; i < cut; i++ {
			if en := ents[i]; en.key == "" || last[en.key] == i { // a real line, not a superseded stub
				latest[en.name] = i
			}
		}
		var keep []int
		for name, i := range latest {
			if !inWindow[name] {
				keep = append(keep, i)
			}
		}
		sort.Ints(keep)
		if len(keep) > evidenceKeptPerTool {
			keep = keep[len(keep)-evidenceKeptPerTool:]
		}
		var kept []string
		for _, i := range keep {
			kept = append(kept, "[kept from before the window — the last "+ents[i].name+" result this turn] "+lines[i])
		}
		dropped := cut - len(kept)
		head := []string{fmt.Sprintf("…%d earlier tool results this turn are not shown", dropped)}
		lines = append(append(head, kept...), lines[cut:]...)
	}
	// The reading rule, stated where the list starts: without it a reader has no way to know the
	// order carries meaning, and the mistake it makes is always the same one — quoting an early
	// snapshot as though nothing after it happened.
	return "(time order, oldest first — a later result outranks an earlier one about the same file or command)\n" +
		"- " + strings.Join(lines, "\n- ")
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
	return strings.TrimSpace(strings.ReplaceAll(resultText(raw), "\n", " ⏎ "))
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
