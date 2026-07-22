package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// turnToolEvidence summarizes THIS turn's tool RESULTS as real, git-independent
// evidence of what actually happened — a write that reported bytes, a `cat` that shows
// the content. It deliberately EXCLUDES the model's own text: that is the agent's claim
// (already passed as Report), and admitting narration as "evidence" is exactly how a
// defeatist agent talks the council into "done" with no artifact (the download-youtube
// lesson). Only events since the last user prompt are considered, so a prior turn's
// successful tool result can't masquerade as this turn's. Most recent k results.
func turnToolEvidence(evs []event.Event, k int) string {
	names := map[string]string{} // callID -> tool name
	var lines []string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted { // new turn boundary → keep only the latest turn's evidence
			names = map[string]string{}
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
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			if d.Part.ToolResult == nil {
				continue
			}
			name := names[d.Part.ToolResult.CallID]
			if name == "" {
				name = "tool"
			}
			status := "ok"
			if d.Part.ToolResult.IsError {
				status = "error"
			}
			lines = append(lines, "tool "+name+" ["+status+"]: "+clipLine(toolResultText(d.Part.ToolResult.Content), councilActionCap))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > k {
		lines = lines[len(lines)-k:]
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
		if e.Type == event.TypePromptSubmitted { // new turn → keep only the latest turn's obstacles
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
		obst = obst[len(obst)-k:]
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
		if e.Type == event.TypePromptSubmitted { // new turn boundary → judge only the latest turn
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
		if e.Type == event.TypePromptSubmitted { // judge only the latest turn
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
	names := map[string]string{}
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
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			if d.Part.ToolResult == nil {
				continue
			}
			name := names[d.Part.ToolResult.CallID]
			if name == "" {
				name = "tool"
			}
			status := "ok"
			if d.Part.ToolResult.IsError {
				status = "error"
			}
			lines = append(lines, "tool "+name+" ["+status+"]: "+clipLine(toolResultText(d.Part.ToolResult.Content), councilActionCap))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > k {
		lines = lines[len(lines)-k:]
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

// subagentTurnEvidence renders the tool evidence of the subagents this orchestrator (parent)
// dispatched DURING THE CURRENT TURN. A delegating orchestrator runs few or no tools itself
// and injects each subagent's result as a prose PROMPT — which turnToolEvidence both excludes
// (it is model text, not a tool result) and treats as a turn boundary (it resets the window).
// So without this the council judges delegated work on the orchestrator's synthesis alone,
// blind to what the subagents actually did. This restores the raw child tool evidence, labeled
// per subagent, so the council sees the real actions. Scoped by creation time to the current
// turn (children from a prior turn linger in a.states and must not leak in). Best-effort: an
// unreadable child is skipped, never an error.
func (a *App) subagentTurnEvidence(ctx context.Context, parent session.SessionID, parentEvs []event.Event) string {
	turnStart := lastUserPromptTS(parentEvs)
	a.mu.Lock()
	var kids []session.Session
	for _, st := range a.states {
		s := st.meta
		if s.Parent != parent {
			continue
		}
		if !turnStart.IsZero() && s.Created.Before(turnStart) {
			continue // a child from a previous turn of this session
		}
		kids = append(kids, s)
	}
	a.mu.Unlock()
	if len(kids) == 0 {
		return ""
	}
	sort.Slice(kids, func(i, j int) bool {
		if !kids[i].Created.Equal(kids[j].Created) {
			return kids[i].Created.Before(kids[j].Created)
		}
		return kids[i].ID < kids[j].ID
	})
	if len(kids) > maxSubagentsShown { // keep the most recent wave when many were dispatched
		kids = kids[len(kids)-maxSubagentsShown:]
	}
	var blocks []string
	for _, k := range kids {
		cevs, err := a.store.Read(ctx, k.ID, 0)
		if err != nil {
			continue
		}
		if m := turnToolEvidence(cevs, subagentActionsCap); m != "" {
			label := k.Agent
			if label == "" {
				label = "subagent"
			}
			blocks = append(blocks, "subagent "+label+":\n"+m)
		}
	}
	return strings.Join(blocks, "\n")
}
