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
func (a *App) maybeCompact(ctx context.Context, s session.Session, agent AgentSpec, actor event.Actor, evs []event.Event, sys string) bool {
	msgs := reconstruct(evs)
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
		TokensBefore:    estimateTokens(sys, msgs),
		TokensAfter:     estimateTokens(summary, reconstruct(keptEvs)),
		Shards:          shards,
	})
	a.appendFact(ctx, s.ID, event.TypeCompaction, actor, d)
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
// tools) or "file" (astgrep / LSP nav), so those land on the right topic, not "discussion".
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

// summarizeViaLLM asks the model to summarize prior conversation into a compact
// brief that preserves decisions, facts, and open tasks. It uses the agent's own
// provider so compaction runs on the same backend the agent is routed to.
func (a *App) summarizeViaLLM(ctx context.Context, agent AgentSpec, s session.Session, msgs []session.Message) string {
	req := port.ChatRequest{
		Model: s.Model.Model,
		System: "Summarize the following conversation into a concise brief that preserves key facts, " +
			"decisions, file changes, and any unfinished tasks. Write only the summary.",
		Messages: msgs,
	}
	stream, err := a.providerFor(agent).StreamChat(ctx, req)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return strings.TrimSpace(b.String())
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
func estimateTokens(sys string, msgs []session.Message) int {
	chars := len(sys)
	for _, m := range msgs {
		for _, p := range m.Parts {
			chars += len(p.Text)
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
