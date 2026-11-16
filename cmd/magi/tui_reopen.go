package main

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// reopenCutConversation is the conversation a standalone terminal reopens: the latest one here, when
// it was cut — the terminal died mid-task (2026-11-16: "SIGKILL, restart, and it starts a new
// conversation; a dead terminal loses the work"). Empty means open a new one, as a normal start does.
//
// Recovery writes into logs, so it runs only when nothing else can be running them: this terminal is
// the only one alive here (holdTUI) and no daemon answers on the workspace socket. A daemon recovers
// its own on start; another terminal's turn is not this one's to close.
//
// The returned release ends this terminal's hold; call it when the UI exits.
func reopenCutConversation(ctx context.Context, a *app.App, wd, sock string) (session.SessionID, func()) {
	release, sole, share := holdTUI(sock)
	defer share()
	if !sole {
		return "", release
	}
	if c, err := daemon.DialWithin(sock, 100*time.Millisecond, 300*time.Millisecond); err == nil {
		c.Close()
		return "", release
	}
	metas, err := a.ListSessions(ctx, wd)
	if err != nil {
		return "", release
	}
	cand := latestConversation(metas)
	if cand == "" {
		return "", release
	}
	// Settles the cut turn in the log (resumable, or handed to the person when a tool may have run)
	// and turns on lazy recovery for /resume; nothing is executed again.
	a.RecoverSelected(ctx, wd, cand)
	if a.RecoveryStateOf(ctx, cand).Kind == app.RecoveryNone {
		return "", release
	}
	return cand, release
}
