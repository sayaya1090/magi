package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// keepRecentEvents is the number of trailing fact events preserved verbatim when
// auto-compacting; older context is summarized.
const keepRecentEvents = 6

// maybeCompact summarizes older context when the estimated token count exceeds
// the model's window budget, returning true if a compaction event was appended.
// (M6 context-aware compaction, building on F-COMPACT.)
// msgs is reconstruct(evs), built once per step by the caller and shared (reconstruct is
// O(events); this sizing check runs every step).
func (a *App) maybeCompact(ctx context.Context, s session.Session, agent AgentSpec, actor event.Actor, evs []event.Event, msgs []session.Message, sys string) bool {
	window := a.contextWindow(s.Model.Model)
	if window <= 0 {
		return false // unknown/unlimited window → no ratio-based auto-compaction
	}
	budget := int(float64(window) * a.cfg.CompactRatio)
	// The provider's real prompt_tokens lags within a turn (it reflects the last completed
	// call), so as a turn accumulates large tool results the trigger would under-count and
	// miss the growth. Use whichever is larger — the real count or the current estimate.
	tokens := a.contextTokens(s.ID, sys, msgs)
	if est := estimateTokens(sys, msgs); est > tokens {
		tokens = est
	}
	if tokens <= budget {
		return false
	}
	return a.compactNow(ctx, s, agent, actor, evs)
}

// compactNow folds older context into a summary down to the last keepRecentEvents facts,
// regardless of any window/budget estimate. maybeCompact calls it once its ratio gate trips;
// the stream loop calls it directly when the provider ITSELF rejects a request as too long —
// the backend's own limit is ground truth that overrides our token estimate (which can under-
// count, or be uncalibrated when the model's real window differs from a hand-set constant).
// Returns false when there is nothing left to fold (already at the minimal kept tail).
func (a *App) compactNow(ctx context.Context, s session.Session, agent AgentSpec, actor event.Actor, evs []event.Event) bool {
	// Find the boundary: keep the last keepRecentEvents fact events verbatim.
	factSeqs := make([]int64, 0, len(evs))
	for _, e := range evs {
		if e.Type.IsFact() {
			factSeqs = append(factSeqs, e.Seq)
		}
	}
	if len(factSeqs) <= keepRecentEvents+1 {
		return false // not enough to compact
	}
	boundary := factSeqs[len(factSeqs)-keepRecentEvents-1]

	// Summarize everything up to the boundary.
	older := reconstruct(truncateAt(evs, boundary))
	if len(older) == 0 {
		return false
	}
	// Index the compacted region into recallable topics (deterministic — by file path,
	// each carrying its tool-action trail as a brief), then write the overall summary.
	shards := shardByPath(older, s.Workdir)
	summary := a.summarizeViaLLM(ctx, agent, s, older)
	if summary == "" {
		return false
	}

	// Post-compaction context = the summary + the kept recent events.
	var keptEvs []event.Event
	for _, e := range evs {
		if e.Seq > boundary {
			keptEvs = append(keptEvs, e)
		}
	}
	d, _ := json.Marshal(event.CompactionData{
		Summary:         summary,
		ReplacesUpToSeq: boundary,
		TokensBefore:    estimateTokens("", reconstruct(evs)),
		TokensAfter:     estimateTokens(summary, reconstruct(keptEvs)),
		Shards:          shards,
	})
	a.appendFact(ctx, s.ID, event.TypeCompaction, actor, d)
	// The real prompt count is now a measurement of a context that no longer exists — and it is
	// the LARGER number, so the trigger (which takes max of the real count and the estimate) would
	// keep firing on the emptied window. The log-derived reader already zeroes it after a fold for
	// exactly this reason; the in-memory trigger was left reading the dead value and re-folding the
	// tail of what little remained, once per turn that ended before a fresh request could refresh
	// it. Cleared here so the next step measures the folded context.
	a.setPromptTokens(s.ID, 0)
	return true
}

// shardByPath groups the compacted messages into recallable topics by the file each
// one touched (read/edited/grepped/etc.), plus a single "discussion" shard for messages
// that reference no file. It is deterministic — no model call and no parse-failure path —
// so the index is always faithful and complete: every recoverable message lands in at
// least one shard, and a topic is a path the agent naturally recalls. Prior summaries
// (id "compaction-*") are skipped — they are not original detail to recover.
func shardByPath(older []session.Message, workdir string) []event.ContextShard {
	callPath := map[string]string{}  // tool callID → relative path, to attribute a result to its call's file
	actions := map[string][]string{} // path → ordered tool names, for a deterministic brief
	for _, m := range older {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolCall && p.ToolCall != nil {
				if rel := shardPath(workdir, p.ToolCall.Args); rel != "" {
					callPath[p.ToolCall.CallID] = rel
					actions[rel] = append(actions[rel], p.ToolCall.Name)
				}
			}
		}
	}

	byPath := map[string][]string{}      // path → ordered message IDs
	seen := map[string]map[string]bool{} // path → set(msgID), dedupe
	var order []string                   // first-seen path order, for stable output
	var discussion []string
	add := func(path, id string) {
		if seen[path] == nil {
			seen[path] = map[string]bool{}
		}
		if seen[path][id] {
			return
		}
		if _, ok := byPath[path]; !ok {
			order = append(order, path)
		}
		byPath[path] = append(byPath[path], id)
		seen[path][id] = true
	}

	for _, m := range older {
		if strings.HasPrefix(m.ID, "compaction-") {
			continue
		}
		paths := map[string]bool{}
		for _, p := range m.Parts {
			switch {
			case p.Kind == session.PartToolCall && p.ToolCall != nil:
				if rel := shardPath(workdir, p.ToolCall.Args); rel != "" {
					paths[rel] = true
				}
			case p.Kind == session.PartToolResult && p.ToolResult != nil:
				if rel := callPath[p.ToolResult.CallID]; rel != "" {
					paths[rel] = true
				}
			}
		}
		if len(paths) == 0 {
			discussion = append(discussion, m.ID)
			continue
		}
		keys := make([]string, 0, len(paths)) // sort so multi-path messages add deterministically
		for k := range paths {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, path := range keys {
			add(path, m.ID)
		}
	}

	shards := make([]event.ContextShard, 0, len(order)+1)
	for _, path := range order {
		shards = append(shards, event.ContextShard{Topic: path, Brief: actionTrail(actions[path]), MessageIDs: byPath[path]})
	}
	if len(discussion) > 0 {
		shards = append(shards, event.ContextShard{
			Topic: "discussion", Brief: "messages not tied to a specific file", MessageIDs: discussion,
		})
	}
	return shards
}

// actionTrail renders a path's tool activity as a deterministic one-line brief, e.g.
// "read · edit×2 · bash" — distinct tools in first-seen order, with a ×N count when
// repeated. Empty when no tools were recorded.
func actionTrail(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var order []string
	count := map[string]int{}
	for _, n := range names {
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if count[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", n, count[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " · ")
}

// shardPath extracts the file a tool call targeted and returns it relative to workdir;
// "" when the call references no file (e.g. bash, web tools). It reads "path" (most file
// tools) or "file", so those land on the right topic, not "discussion".
func shardPath(workdir string, args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &a)
	p := a.Path
	if p == "" {
		p = a.File
	}
	if p == "" {
		return ""
	}
	return relForChange(workdir, p)
}

// truncateAt returns events with seq <= boundary.
func truncateAt(evs []event.Event, boundary int64) []event.Event {
	out := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		if e.Seq <= boundary {
			out = append(out, e)
		}
	}
	return out
}

// flattenForSummary renders a conversation as plain text: every tool call and tool result becomes
// a PartText line, so the summarizer request carries no tool_use structure and needs no Tools
// declared. Roles are preserved; only the shape of each part changes.
func flattenForSummary(msgs []session.Message) []session.Message {
	out := make([]session.Message, 0, len(msgs))
	for _, m := range msgs {
		var text []string
		for _, p := range m.Parts {
			switch {
			case p.Kind == session.PartText && p.Text != "":
				text = append(text, p.Text)
			case p.ToolCall != nil:
				text = append(text, fmt.Sprintf("[called %s %s]", p.ToolCall.Name, string(p.ToolCall.Args)))
			case p.ToolResult != nil:
				text = append(text, fmt.Sprintf("[result: %s]", string(p.ToolResult.Content)))
			}
		}
		joined := strings.TrimSpace(strings.Join(text, "\n"))
		if joined == "" {
			continue
		}
		// The assistant's own turns stay the assistant's; a tool-result message would be an
		// illegal tool role with no tool_call to answer once flattened, so it rides in as user
		// context — which is all a summarizer needs it to be.
		role := m.Role
		if role != session.RoleAssistant {
			role = session.RoleUser
		}
		out = append(out, session.Message{Role: role, Parts: []session.Part{{Kind: session.PartText, Text: joined}}})
	}
	return out
}

// summarizeViaLLM asks the model to summarize prior conversation into a compact
// brief that preserves decisions, facts, and open tasks. It uses the agent's own
// provider so compaction runs on the same backend the agent is routed to.
func (a *App) summarizeViaLLM(ctx context.Context, agent AgentSpec, s session.Session, msgs []session.Message) string {
	prov := a.providerFor(agent)
	if prov == nil {
		// The same guard generate_step grew: a reader-only App has no backend, and dereferencing
		// nil here took the process down. Nothing to summarize on is a failure to report, not a
		// crash.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			"compact: no model backend to summarize with, so this fold was skipped")
		return ""
	}
	req := port.ChatRequest{
		Model: s.Model.Model,
		System: "Summarize the following conversation into a concise brief that preserves key facts, " +
			"decisions, file changes, and any unfinished tasks. Write only the summary.",
		// Tool calls and results flattened to plain text. The request carries no Tools, and a strict
		// backend (Anthropic via a gateway) rejects a message holding tool_use blocks with no tools
		// declared — so every auto-fold on such a route failed silently, forever. The summarizer
		// only needs to READ the conversation; rendered as prose it says the same thing and goes out
		// legally on any backend.
		Messages: flattenForSummary(msgs),
	}
	stream, err := prov.StreamChat(ctx, req)
	if err != nil {
		// A provider error here is not "nothing to fold" — which is what returning "" tells the
		// caller. Left silent, the caller re-tried the same doomed call every step and the turn
		// died with a raw overflow no one could connect to a failing compaction. Reported, and
		// still returns "" so the caller keeps the history rather than replacing it with nothing.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			fmt.Sprintf("compact: the summarizer was refused (%v) — history kept, not folded", err))
		return ""
	}
	text, cut := drainStream(stream)
	if cut != nil {
		// This summary BECOMES the session's memory of everything it replaces, so a half-written
		// one silently discards conversation. Report it; the caller still uses what arrived rather
		// than losing the region entirely.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			fmt.Sprintf("compact: the summary was CUT OFF after %d chars — %v (the compacted region is summarized incompletely)", len(text), cut))
		// …and say it INSIDE the summary. The line above goes to the operator; the reader who
		// depends on this text is the agent, and for it this paragraph is now the whole of what
		// happened before. Every other truncation in magi marks itself in the artifact it cut —
		// the result cap, the capture head/tail, the evidence block's dropped tail — because a
		// reader cannot ask for what it does not know is missing. This one only told the console.
		text = strings.TrimSpace(text)
		if text != "" {
			text += "\n\n[this summary is INCOMPLETE — the model was cut off while writing it, so " +
				"parts of what it replaces are described nowhere. Treat gaps as unknown rather than " +
				"as nothing having happened; recall_context can re-open the detail by topic.]"
		}
		return text
	}
	return strings.TrimSpace(text)
}

// contextTokens reports the context size: the provider's real prompt_tokens from
// the last turn when available, otherwise a chars/4 estimate. Real counts make
// the meter and compaction trigger accurate.
func (a *App) contextTokens(sid session.SessionID, sys string, msgs []session.Message) int {
	if n := a.realPromptTokens(sid); n > 0 {
		return n
	}
	return estimateTokens(sys, msgs)
}

// estimateTokens approximates the token count of a request (≈4 chars/token).
//
// It must count what the WIRE carries, not what the session holds. Reasoning parts are persisted
// every step and rebuilt on replay, but the openai adapter's joinText sends only PartText — so
// counting a reasoning part's Text here inflated the estimate by the bulk of a thinking model's
// output (routinely several times the text), and the trigger takes max(real, estimate). The result
// was a session folded away at 15-20% of real window use, and re-folded on later turns for the
// same phantom reason. Count PartText, tool calls and tool results; skip the reasoning that never
// leaves the machine.
func estimateTokens(sys string, msgs []session.Message) int {
	chars := len(sys)
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartText {
				chars += len(p.Text)
			}
			if p.ToolCall != nil {
				chars += len(p.ToolCall.Name) + len(p.ToolCall.Args)
			}
			if p.ToolResult != nil {
				chars += len(p.ToolResult.Content)
			}
		}
	}
	return chars / 4
}
