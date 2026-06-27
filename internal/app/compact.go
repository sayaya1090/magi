package app

import (
	"context"
	"encoding/json"
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
	window := a.cfg.Models.Get(s.Model.Model).ContextWindow
	budget := int(float64(window) * a.cfg.CompactRatio)
	if a.contextTokens(s.ID, sys, msgs) <= budget {
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
	})
	a.appendFact(ctx, s.ID, event.TypeCompaction, actor, d)
	return true
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
