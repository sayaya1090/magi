// Package tui implements the interactive terminal UI (M2) using Bubble Tea,
// Lipgloss, and Glamour. It is an adapter over the application service: it
// subscribes to the event bus for live updates and issues commands on input.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Run starts the interactive TUI for a session and blocks until the user quits.
// isDark selects the color theme. The model subscribes to the session itself
// (see Init/startSub) so it can switch sessions on /resume.
func Run(ctx context.Context, a *app.App, cmds CommandSource, sid session.SessionID, model, workdir string, isDark bool, imageProto string) error {
	// Match our display-width measure to the host terminal (mainly for Windows,
	// where ambiguous-width glyphs are often drawn wide) so the scrollbar gutter
	// stays aligned on lines with special characters. Best-effort; see width.go.
	detectAmbiguousWidth()
	m := New(ctx, a, cmds, sid, model, workdir, isDark, imageProto)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
