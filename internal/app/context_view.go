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
	// From ContextStateOf, which is the console's reading too.
	//
	// This function used to work the numbers out itself, and the two answers drifted the moment one
	// was fixed: `contextTokens(sid, "", msgs)` passes an EMPTY system prompt, so the terminal was
	// reporting the conversation and calling it the context — measured at 2,404 tokens of system
	// prompt and 5,703 of tool catalog left out of a reading of 8,750. Two surfaces answering one
	// question from two derivations is the arrangement that guarantees exactly one of them gets
	// fixed.
	st, err := a.ContextStateOf(ctx, sid)
	if err != nil {
		return "", err
	}
	// Still needed here and not in the state: the seq the last compaction replaced up to, which is
	// a debugging handle for a person reading a log beside this, and no screen shows it.
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	var lastReplaces int64
	for _, e := range evs {
		if e.Type == event.TypeCompaction {
			var d event.CompactionData
			if json.Unmarshal(e.Data, &d) == nil {
				lastReplaces = d.ReplacesUpToSeq
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Context window — %s\n", orDash(st.Model))
	if st.Window > 0 {
		pct := st.Used * 100 / st.Window
		fmt.Fprintf(&b, "  used ~%s / %s tokens (%d%%)\n", commas(st.Used), commas(st.Window), pct)
	} else {
		fmt.Fprintf(&b, "  used ~%s tokens (window unknown)\n", commas(st.Used))
	}
	// What it is made of, when this session has recorded it. The two biggest pieces — the system
	// prompt and the tool catalog — are not in the log at all, so a session that has not finished a
	// turn under a build that records them says nothing here rather than implying five zeros.
	if sum := st.Parts.Sum(); sum > 0 {
		fmt.Fprintf(&b, "  of that: %s system · %s tools · %s talk · %s calls · %s results%s\n",
			commas(st.Parts.System), commas(st.Parts.Tools), commas(st.Parts.Talk),
			commas(st.Parts.Calls), commas(st.Parts.Results),
			map[bool]string{true: "", false: " (estimated)"}[st.Estimated])
	}
	fmt.Fprintf(&b, "  messages: %d\n", st.Messages)
	if st.Compactions > 0 {
		fmt.Fprintf(&b, "  compactions: %d (last replaced ≤ seq %d, %s→%s tok)\n",
			st.Compactions, lastReplaces, commas(st.LastBefore), commas(st.LastAfter))
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
