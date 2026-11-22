package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// PluginFinish is a plugin tool ending the turn it was called in (magi.finish). The loop reads
// the mark right after the step's calls ran; see turnControl.finishNow.
func (a *App) PluginFinish(sessionID string) {
	if sessionID == "" {
		return
	}
	a.signalTurnControl(session.SessionID(sessionID), func(tc *turnControl) { tc.finishNow = true })
}

// takeFinishNow reads and clears the tool-ended mark, leaving the rest of the control alone.
func (a *App) takeFinishNow(sid session.SessionID) bool {
	now := false
	a.signalTurnControl(sid, func(tc *turnControl) { now, tc.finishNow = tc.finishNow, false })
	return now
}

// PluginNote appends a system note from a plugin to the session transcript —
// the plugin host's magi.notify. It uses the established system-actor prompt
// pattern (council/planner notes), so it renders as a ⟳ note, never counts as
// an unanswered user prompt, and the model sees it next turn (an "engram saved
// skill X — reply N to undo" notice is actionable precisely because the model
// and the user both see it).
func (a *App) PluginNote(sessionID, text string) {
	text = strings.TrimSpace(text)
	if sessionID == "" || text == "" {
		return
	}
	a.appendBestEffort(context.Background(), session.SessionID(sessionID), event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "plugin"}, pluginNoteData(text))
}

// PluginCommandNote records command output as a harness row in the issuing conversation.
func (a *App) PluginCommandNote(sessionID, text string) bool {
	if sessionID == "" || strings.TrimSpace(text) == "" {
		return false
	}
	return a.appendFact(context.Background(), session.SessionID(sessionID), event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "plugin-command"}, pluginNoteData(strings.TrimSpace(text))) == nil
}

func pluginNoteData(text string) json.RawMessage {
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	return pd
}

// RegisterContextProvider adds a context provider for RAG-like context injection.
func (a *App) RegisterContextProvider(p port.ContextProvider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.contextProviders = append(a.contextProviders, p)
	for _, st := range a.states {
		st.ragQ, st.ragText = "", "" // the new provider must be consulted on the next lookup
	}
}

// RegisterCompactionAdvisor adds an extension consulted before a fold or a condense.
func (a *App) RegisterCompactionAdvisor(p port.CompactionAdvisor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.compactionAdvisors = append(a.compactionAdvisors, p)
}

// compactionAdviceBudget caps the bytes extensions may add to one summariser prompt.
const compactionAdviceBudget = 4000

// compactionGuidance asks every registered advisor what the summariser should know about this
// session and returns it as one section for the fold or condense prompt (empty if none said
// anything). Each advisor gets five seconds; one that fails or times out is skipped, so an
// extension can never stop a compaction.
func (a *App) compactionGuidance(ctx context.Context, s session.Session) string {
	a.mu.Lock()
	advisors := append([]port.CompactionAdvisor(nil), a.compactionAdvisors...)
	a.mu.Unlock()
	var parts []string
	size := 0
	for _, adv := range advisors {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		text, err := adv.AdviseCompaction(cctx, port.CompactionQuery{SessionID: s.ID, Workdir: s.Workdir})
		cancel()
		text = strings.TrimSpace(text)
		if err != nil || text == "" {
			continue
		}
		if size+len(text) > compactionAdviceBudget {
			text = strings.TrimSpace(truncateContextBytes(text, compactionAdviceBudget-size))
		}
		if text == "" {
			break
		}
		parts = append(parts, text)
		if size += len(text); size >= compactionAdviceBudget {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\nGuidance from extensions for this brief (follow it; the sections above still apply):\n" + strings.Join(parts, "\n\n")
}

// contextBudget caps the bytes of provider-injected context per turn so a
// chatty RAG source can't blow the window.
const contextBudget = 8000

// gatherContext queries every registered context provider for the current
// request and returns their chunks formatted for the system prompt (empty if
// none). Providers receive a five-second cancellation deadline and must cooperate
// with context cancellation. Collection is sequential.
func (a *App) gatherContext(ctx context.Context, q port.ContextQuery) string {
	a.mu.Lock()
	providers := append([]port.ContextProvider(nil), a.contextProviders...)
	a.mu.Unlock()
	if len(providers) == 0 {
		return ""
	}

	var b strings.Builder
	var decisions []contextDecision
	defer func() {
		a.mu.Lock()
		a.stateLocked(q.SessionID).ragDecisions = decisions
		a.mu.Unlock()
	}()
	for pi, p := range providers {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		chunks, err := p.Provide(cctx, q)
		cancel()
		if err != nil {
			decisions = append(decisions, contextDecision{ID: fmt.Sprintf("tail/retrieved/%d", pi), Lane: volatileStable, State: "skipped", Reason: "provider_error"})
			continue // a failing provider must not break the turn
		}
		for ci, c := range chunks {
			text := strings.TrimSpace(c.Text)
			if text == "" {
				continue
			}
			if c.Source != "" {
				text = "## " + c.Source + "\n" + text
			}
			piece, ds := assembleContext([]contextFragment{{id: fmt.Sprintf("tail/retrieved/%d/%d", pi, ci), source: c.Source, lane: volatileStable, text: text + "\n\n"}}, nil)
			decisions = append(decisions, ds...)
			if b.Len()+len(piece) > contextBudget {
				decisions[len(decisions)-1].State = "truncated"
				decisions[len(decisions)-1].Reason = "byte_budget"
			}
			b.WriteString(piece)
			if b.Len() >= contextBudget {
				return strings.TrimSpace(truncateContextBytes(b.String(), contextBudget))
			}
		}
	}
	return strings.TrimSpace(b.String())
}
