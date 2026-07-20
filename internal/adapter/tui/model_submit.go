package tui

// The input-submission pipeline: plain submits, steer-while-running, the
// `!`-inline-shell escape, @path mention expansion, and the workdir jail the
// expansions read through. Pure moves from model.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// submit sends a user prompt, expanding @file mentions and collapsed-paste
// placeholders into full content. The transcript shows the FULL pasted content
// (the input box is cramped, the main view isn't); history keeps the collapsed
// chip so ↑ recall doesn't dump the blob back into the input.
func (m *Model) submit(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIdx = len(m.history)
	return m.submitAs(m.expandPastes(text), m.expandPastes(m.expandMentions(text)))
}

// submitAs displays one text in the transcript but sends another to the agent
// (used by @-mention expansion and /init). It clears the input box.
func (m *Model) submitAs(display, send string) tea.Cmd {
	m.ta.Reset()
	return m.sendPrompt(display, send)
}

// steer injects a message into the running turn (the agent picks it up at its
// next step) instead of queuing it. The message appears in the transcript
// immediately; the running spinner keeps going.
func (m *Model) steer(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIdx = len(m.history)
	send := m.expandPastes(m.expandMentions(text))
	m.blocks = append(m.blocks, block{kind: blockUser, text: m.expandPastes(text), queued: true, ts: time.Now()})
	m.ta.Reset()
	m.refresh()
	sid := m.sid
	// Be honest about routing: the engine QUEUES a mid-turn message by default and
	// runs it as its own turn after the current one finishes (the agent may fold it
	// in sooner). Saying "steered into the running turn" implied immediate handling
	// and left users unsure whether the current task was abandoned or the new one lost.
	note := "queued · runs after the current task finishes (agent may fold it in sooner)"
	if m.anyPaneRunning() {
		note = "queued · the main agent picks it up after the running subagents finish this step"
	}
	return tea.Batch(m.snack(note), func() tea.Msg {
		_ = m.app.Steer(m.ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: send}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	})
}

// shellRun is one `!`-executed command and its captured output, staged to be
// folded into the next prompt's context (Claude-style inline shell).
type shellRun struct {
	cmd  string
	out  string
	exit int
}

// maxShellOut caps the bytes of a single `!` command's output kept for both the
// transcript and the injected context, so a chatty command can't blow the window.
const maxShellOut = 16 << 10

// runShellBang executes a `!`-prefixed command in the workdir, renders it as a
// blockShell, and stages the output for the next prompt — or, mid-turn, steers it
// straight into the running turn so the agent sees it at its next step.
func (m *Model) runShellBang(cmd string) tea.Cmd {
	m.ta.Reset()
	m.history = append(m.history, "!"+cmd)
	m.histIdx = len(m.history)
	m.refresh()

	// Run off the Bubble Tea Update goroutine so a slow command (`!npm ci`, `!sleep`)
	// doesn't freeze the whole TUI; the result returns as a shellResultMsg.
	ctx, workdir, app := m.ctx, m.workdir, m.app
	return func() tea.Msg {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, exit, err := app.RunShell(rctx, workdir, cmd)
		if err != nil {
			out, exit = "error: "+err.Error(), -1
		}
		return shellResultMsg{cmd: cmd, out: out, exit: exit}
	}
}

// applyShellResult records a finished `!` run: it appends the transcript block and
// either steers the output into an in-flight turn or stages it for the next prompt.
func (m *Model) applyShellResult(msg shellResultMsg) tea.Cmd {
	out := msg.out
	if capped, cut := capBytes(out, maxShellOut); cut {
		out = capped + "\n…(output truncated)"
	}
	run := shellRun{cmd: msg.cmd, out: out, exit: msg.exit}
	m.blocks = append(m.blocks, block{
		kind:   blockShell,
		args:   msg.cmd,
		text:   out,
		result: fmt.Sprintf("exit %d", msg.exit),
		ok:     msg.exit == 0,
	})
	if m.running {
		// A turn is in flight — inject immediately (steer) instead of staging.
		send := shellContext([]shellRun{run})
		sid := m.sid
		m.refresh()
		return tea.Batch(m.snack("ran !"+oneLine(msg.cmd, 40)+" — steered into the turn"), func() tea.Msg {
			_ = m.app.Steer(m.ctx, command.SubmitPrompt{
				SessionID: sid,
				Parts:     []session.Part{{Kind: session.PartText, Text: send}},
				Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
			})
			return nil
		})
	}
	m.pendingShell = append(m.pendingShell, run)
	m.refresh()
	return nil
}

// drainPendingShell formats and clears the staged `!` runs as a context preamble
// to prepend to the next prompt (empty when nothing is staged).
func (m *Model) drainPendingShell() string {
	if len(m.pendingShell) == 0 {
		return ""
	}
	pre := shellContext(m.pendingShell)
	m.pendingShell = nil
	return pre
}

// shellContext renders staged shell runs as a plain-text preamble the agent reads
// as part of the next user message.
func shellContext(runs []shellRun) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString("I ran a shell command:\n$ ")
		b.WriteString(r.cmd)
		b.WriteString("\n")
		if out := strings.TrimRight(r.out, "\n"); out != "" {
			b.WriteString(out)
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "(exit %d)\n\n", r.exit)
	}
	return b.String()
}

// capBytes truncates s to at most n bytes, dropping any partial trailing rune, and
// reports whether it cut anything.
func capBytes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return strings.ToValidUTF8(s[:n], ""), true
}

// safeWhileRunning reports whether a slash command is safe to run during an
// in-flight turn (read-only / UI-only — does not mutate the running session).
func safeWhileRunning(cmd string) bool {
	switch cmd {
	case "/help", "/model", "/agents", "/route", "/tools", "/sessions", "/diff", "/loop", "/loopdiff", "/context", "/subagent", "/permission":
		return true
	}
	return false
}

// sendPrompt appends a user block and dispatches the prompt without touching the
// input box, so flushing the queue never clobbers what the user is now typing.
func (m *Model) sendPrompt(display, send string) tea.Cmd {
	m.closePanes() // a new turn retires the previous turn's subagent panes
	// Fold in any `!`-run shell output the user staged before this prompt, so the
	// agent sees it in context (the display keeps only what the user typed).
	if pre := m.drainPendingShell(); pre != "" {
		send = pre + send
	}
	m.blocks = append(m.blocks, block{kind: blockUser, text: display, ts: time.Now()})
	m.running = true
	m.awaitingTurnReqID = true // the next ActorUser prompt.submitted owns this turn's spinner
	m.turnStart = time.Now()   // §8.1: start the elapsed/token meter
	m.turnIn, m.turnOut, m.turnDur = 0, 0, 0
	m.turnSteps, m.turnCouncil, m.turnFiles = 0, 0, map[string]bool{}
	m.turnUnverified = false
	m.refresh()
	sid := m.sid
	return tea.Batch(m.sp.Tick, func() tea.Msg {
		_ = m.app.Submit(m.ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: send}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	})
}

// mentionRE matches @-prefixed file paths.
var mentionRE = regexp.MustCompile(`@([^\s]+)`)

// expandMentions appends the contents of any @-mentioned files that exist in the
// workdir, so the agent has them in context (@ file mentions).
func (m *Model) expandMentions(text string) string {
	matches := mentionRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	seen := map[string]bool{}
	for _, mt := range matches {
		rel := mt[1]
		if seen[rel] {
			continue
		}
		seen[rel] = true
		content, ok := m.readWorkdirFile(rel)
		if !ok {
			continue
		}
		b.WriteString("\n\n--- " + rel + " ---\n" + content)
	}
	return b.String()
}

// jailPath resolves a (possibly relative) path inside the workdir, rejecting
// escapes. Returns the absolute path.
func (m *Model) jailPath(rel string) (string, bool) {
	base := filepath.Clean(m.workdir)
	abs := filepath.Clean(filepath.Join(base, rel))
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	}
	if r, err := filepath.Rel(base, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// readWorkdirImagePath returns the jailed absolute path for an image file.
func (m *Model) readWorkdirImagePath(rel string) (string, bool) { return m.jailPath(rel) }

// readWorkdirFile reads a workdir-relative file (jailed), capped at 50KB.
func (m *Model) readWorkdirFile(rel string) (string, bool) {
	abs, ok := m.jailPath(rel)
	if !ok {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	const cap = 50 * 1024
	if len(data) > cap {
		return string(data[:cap]) + "\n…(truncated)", true
	}
	return string(data), true
}
