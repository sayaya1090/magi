package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

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
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(context.Background(), session.SessionID(sessionID), event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "plugin"}, pd)
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

// contextBudget caps the characters of provider-injected context per turn so a
// chatty RAG source can't blow the window.
const contextBudget = 8000

// gatherContext queries every registered context provider for the current
// request and returns their chunks formatted for the system prompt (empty if
// none). Each provider is bounded by a short timeout so a slow or hung source
// degrades to "no extra context" instead of stalling the turn.
func (a *App) gatherContext(ctx context.Context, q port.ContextQuery) string {
	a.mu.Lock()
	providers := append([]port.ContextProvider(nil), a.contextProviders...)
	a.mu.Unlock()
	if len(providers) == 0 {
		return ""
	}

	var b strings.Builder
	for _, p := range providers {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		chunks, err := p.Provide(cctx, q)
		cancel()
		if err != nil {
			continue // a failing provider must not break the turn
		}
		for _, c := range chunks {
			text := strings.TrimSpace(c.Text)
			if text == "" {
				continue
			}
			if c.Source != "" {
				b.WriteString("## " + c.Source + "\n")
			}
			b.WriteString(text + "\n\n")
			if b.Len() >= contextBudget {
				return strings.TrimSpace(b.String()[:contextBudget])
			}
		}
	}
	return strings.TrimSpace(b.String())
}
