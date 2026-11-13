package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// recoveryChip is the header's word on what the last stop left in this conversation. It stays
// until the person has dealt with it: a snackbar said it once and was gone before anyone looked.
func (m *Model) recoveryChip() string {
	switch m.recovery.Kind {
	case app.RecoveryResumable:
		return styleKeyLabel.Render("⟲ cut turn — send continue")
	case app.RecoveryNeedsCheck:
		return styleError.Render(fmt.Sprintf("⚠ check %d call(s) — /recover", len(m.recovery.Uncertain)))
	case app.RecoveryFailed:
		return styleError.Render("✖ recovery failed — /recover retry")
	}
	return ""
}

// refreshRecoveryAfterTurn re-reads the state when a turn ends, but only when there was something
// to say: a new prompt is what settles a cut turn, and reading the log on every ordinary turn
// would cost a whole log read to learn "still nothing".
func (m *Model) refreshRecoveryAfterTurn() {
	if m.recovery.Kind == app.RecoveryNone || m.recovery.Kind == "" {
		return
	}
	m.recovery = m.app.RecoveryStateOf(m.ctx, m.sid)
}

// verdictWords are what a person types; the log keeps the canonical names.
var verdictWords = map[string]app.Verdict{
	"ran": app.VerdictRan, "not_ran": app.VerdictNotRan, "notran": app.VerdictNotRan,
	"rerun": app.VerdictRerunOK, "rerun_ok": app.VerdictRerunOK,
	"defer": app.VerdictDeferred, "deferred": app.VerdictDeferred,
}

// handleRecover is /recover: list what the restart left, record the person's word on one call, or
// look again at a conversation magi could not read. Nothing here re-runs anything — a verdict only
// says what is true, and continuing stays the person's next message.
func (m *Model) handleRecover(args []string) tea.Cmd {
	m.recovery = m.app.RecoveryStateOf(m.ctx, m.sid)
	if len(args) == 0 {
		return m.listRecovery()
	}
	if args[0] == "retry" {
		r := m.app.RetryRecovery(m.ctx, m.sid)
		m.recovery = m.app.RecoveryStateOf(m.ctx, m.sid)
		m.refresh()
		if r.Err != nil {
			return m.snack("still could not read this conversation: " + r.Err.Error())
		}
		return m.snack("recovered — " + string(m.recovery.Kind))
	}
	if len(args) < 2 {
		return m.snack("usage: /recover N ran|not_ran|rerun|defer [note]")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(m.recovery.Uncertain) {
		return m.snack(fmt.Sprintf("no call %s to settle — /recover lists %d", args[0], len(m.recovery.Uncertain)))
	}
	v, ok := verdictWords[strings.ToLower(args[1])]
	if !ok {
		return m.snack("say what happened: ran, not_ran, rerun or defer")
	}
	c := m.recovery.Uncertain[n-1]
	if err := m.app.ResolveCall(m.ctx, m.sid, c.CallID, v, strings.Join(args[2:], " ")); err != nil {
		return m.snack("not recorded: " + err.Error())
	}
	m.recovery = m.app.RecoveryStateOf(m.ctx, m.sid)
	m.refresh()
	return m.snack(fmt.Sprintf("%s %s marked %s", c.Tool, c.Target, v))
}

func (m *Model) listRecovery() tea.Cmd {
	st := m.recovery
	switch st.Kind {
	case app.RecoveryResumable:
		m.info("The last turn was cut by a stop and nothing in it needs checking. Send \"continue\" (or anything else) to carry on from the last settled point.")
	case app.RecoveryFailed:
		m.info("magi could not read this conversation when it started, so new work here is held back.\n  why: " +
			st.Reason + "\n  /recover retry   look again")
	case app.RecoveryNeedsCheck:
		var b strings.Builder
		b.WriteString("These calls had started when magi stopped. They may have done their work; nothing re-ran them.\n")
		for i, c := range st.Uncertain {
			fmt.Fprintf(&b, "  %d. %s %s", i+1, c.Tool, c.Target)
			if c.Verdict != "" {
				fmt.Fprintf(&b, "  (%s)", c.Verdict)
			}
			if h := checkHint(c); h != "" {
				b.WriteString("\n     check: " + h)
			}
			b.WriteString("\n")
		}
		b.WriteString("  /recover 1 ran|not_ran|rerun|defer [note]   say what you found")
		m.info(b.String())
	default:
		return m.snack("the last stop left nothing to check here")
	}
	m.refresh()
	return nil
}

// checkHint is the safe way to find out, offered and never run: looking at a file costs nothing,
// a command or a request is only described, because repeating it is the thing being decided.
func checkHint(c app.UncertainCall) string {
	switch {
	case c.Target == "":
		return ""
	case c.Tool == "write" || c.Tool == "edit" || c.Tool == "multiedit":
		return "open " + c.Target + " (or /diff) and see whether the change is there"
	case c.Tool == "bash":
		return "look for what the command would have changed; repeating it runs it twice"
	case strings.HasPrefix(c.Target, "http"):
		return "a repeat sends the request again; check the other side first"
	}
	return ""
}

// resumeBadgeOf is the picker's mark for a conversation the last stop left something in. Filled
// on first draw of the row and kept for as long as the picker is open.
func (m *Model) resumeBadgeOf(sid session.SessionID) string {
	if m.resumeBadge == nil {
		return ""
	}
	if b, ok := m.resumeBadge[sid]; ok {
		return b
	}
	b := ""
	switch st := m.app.RecoveryStateOf(m.ctx, sid); st.Kind {
	case app.RecoveryResumable:
		b = "⟲"
	case app.RecoveryNeedsCheck:
		b = fmt.Sprintf("⚠%d", len(st.Uncertain))
	case app.RecoveryFailed:
		b = "✖"
	}
	m.resumeBadge[sid] = b
	return b
}
