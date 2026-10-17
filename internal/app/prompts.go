package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// PastPrompt is one thing a person typed to this workspace's companion.
type PastPrompt struct {
	Text    string
	At      time.Time
	Session session.SessionID
}

// promptScanSessions bounds how many conversations RecentPrompts opens. Each one is a full log
// read; the newest are read first, so the bound only ever drops the oldest history.
const promptScanSessions = 40

// RecentPrompts is what a person typed to this workspace's companion, newest first, across
// conversations, each text once (its newest use).
//
// It is the ↑/↓ recall every screen offers in its composer. It used to be each screen's own memory
// of the conversation on screen: the terminal seeded it from that one session and threw it away on
// a switch, the IDE windows had none, so the line typed yesterday in another conversation was gone
// (2026-10-17, asked for in every client). The conversations already hold every prompt — this reads
// them rather than keeping a second copy that could disagree.
//
// Only what a PERSON typed (typedPrompt): not magi's own notes, not a subagent's report or work
// handed over by another companion, not a scheduled run's prompt — that was written once into a
// schedule, not typed.
func (a *App) RecentPrompts(ctx context.Context, workdir string, limit int) ([]PastPrompt, error) {
	if limit <= 0 {
		limit = 100
	}
	metas, err := a.ListLiveSessions(ctx, workdir)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	var out []PastPrompt
	seen := map[string]bool{}
	scanned := 0
	for _, m := range metas {
		if len(out) >= limit || scanned >= promptScanSessions {
			break
		}
		if m.Parent != "" || strings.HasPrefix(m.Origin, "cron:") {
			continue
		}
		scanned++
		evs, err := a.store.Read(ctx, m.ID, 0)
		if err != nil {
			continue // one unreadable log costs its own lines, not the whole history
		}
		for i := len(evs) - 1; i >= 0 && len(out) < limit; i-- {
			t, ok := typedPrompt(evs[i])
			if !ok || seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, PastPrompt{Text: t, At: evs[i].TS, Session: m.ID})
		}
	}
	return out, nil
}

// typedPrompt is the text of a prompt a person typed, or false. Read off the event rather than the
// rebuilt message, because the event names its actor: a subagent's report and work another
// companion handed over arrive as user-role messages too, and neither was typed here.
func typedPrompt(ev event.Event) (string, bool) {
	if ev.Type != event.TypePromptSubmitted || ev.Actor.Kind != event.ActorUser {
		return "", false
	}
	var d event.PromptSubmittedData
	if json.Unmarshal(ev.Data, &d) != nil {
		return "", false
	}
	var b strings.Builder
	for _, p := range d.Parts {
		// The attachment block is the core's rendering of files the person pointed at, not words
		// they typed — recalled, it would refill the composer with the excerpt.
		if p.Kind == session.PartText && !strings.HasPrefix(p.Text, attachedHeader) {
			b.WriteString(p.Text)
		}
	}
	t := strings.TrimSpace(b.String())
	if t == "" || strings.HasPrefix(t, "[subagent ") {
		return "", false
	}
	return t, true
}
