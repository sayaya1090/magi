package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// ContextView returns a human-readable breakdown of what currently fills the
// model's context window for a session — usage vs the window, message count, and
// compaction history — so the otherwise-opaque context is legible (loop-
// engineering pain #6). "used" is the last turn's real prompt tokens when known,
// else a ~4-chars/token estimate.
func (a *App) ContextView(ctx context.Context, sid session.SessionID) (string, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	msgs := reconstruct(evs)
	s := a.sessionInfo(ctx, sid)
	window := a.contextWindow(s.Model.Model)
	used := a.contextTokens(sid, "", msgs)

	var compactions int
	var lastReplaces int64
	var before, after int
	for _, e := range evs {
		if e.Type == event.TypeCompaction {
			var d event.CompactionData
			if json.Unmarshal(e.Data, &d) == nil {
				compactions++
				lastReplaces, before, after = d.ReplacesUpToSeq, d.TokensBefore, d.TokensAfter
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Context window — %s\n", orDash(s.Model.Model))
	if window > 0 {
		pct := used * 100 / window
		fmt.Fprintf(&b, "  used ~%s / %s tokens (%d%%)\n", commas(used), commas(window), pct)
	} else {
		fmt.Fprintf(&b, "  used ~%s tokens (window unknown)\n", commas(used))
	}
	fmt.Fprintf(&b, "  messages: %d\n", len(msgs))
	if compactions > 0 {
		fmt.Fprintf(&b, "  compactions: %d (last replaced ≤ seq %d, %s→%s tok)\n",
			compactions, lastReplaces, commas(before), commas(after))
	} else {
		b.WriteString("  compactions: none\n")
	}
	if td := a.Todos(sid); len(td) > 0 {
		fmt.Fprintf(&b, "  plan: %d todo(s)\n", len(td))
	}

	// Models in use (session + per-agent routes + profiles) with each window —
	// different agents can run different models, so their windows differ. Edit any
	// one with `/context <model> <tokens>`.
	if mws := a.ContextWindows(ctx, sid); len(mws) > 0 {
		b.WriteString("\nmodels in use (edit: /context <model> <tokens>):\n")
		for _, mw := range mws {
			win := "unlimited"
			if mw.Window > 0 {
				win = commas(mw.Window) + " tok"
			}
			marker := " "
			if mw.Session {
				marker = "*"
			}
			fmt.Fprintf(&b, "  %s %-28s %s\n", marker, mw.Model, win)
		}
		b.WriteString("  (* = session model)\n")
	}
	b.WriteString("  (used = last real prompt tokens if known, else estimate)")
	return b.String(), nil
}

// commas formats an int with thousands separators (e.g. 12345 → "12,345").
func commas(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
