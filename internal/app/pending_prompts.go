package app

import (
	"github.com/sayaya1090/magi/internal/core/session"
	"sort"
)

// PendingPrompt exposes delegated-session questions to the local operator,
// independently of whichever chat is selected. Answers still use the source ID.
type PendingPrompt struct {
	Session   session.SessionID
	Requester string
	Ask       Ask
}

func (a *App) PendingPrompts() []PendingPrompt {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []PendingPrompt
	for sid, st := range a.states {
		if st.handedFrom == "" {
			continue
		}
		for _, ask := range st.asking {
			out = append(out, PendingPrompt{Session: sid, Requester: st.handedFrom, Ask: ask})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ask.Since.Equal(out[j].Ask.Since) {
			return string(out[i].Session)+out[i].Ask.ID < string(out[j].Session)+out[j].Ask.ID
		}
		return out[i].Ask.Since.Before(out[j].Ask.Since)
	})
	return out
}
