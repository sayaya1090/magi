package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/sayaya1090/magi/internal/port"
)

// Bounds for a reader of past work: how many of the newest top-level sessions, and how many of the
// latest calls in each. Older work is left unread rather than paged, because the reader is a background
// review that runs again later.
const (
	recentStepSessionsMax = 16
	recentStepsPerSession = 300
)

// RecentToolSteps returns the tool calls of the newest top-level sessions in workdir, newest session
// first, each call with its arguments, its result (an OK result clipped as in turn steps) and the user
// turn it belongs to. Another workspace's sessions are never included: the store lists by workdir.
func (a *App) RecentToolSteps(ctx context.Context, workdir string, sessions int) ([]port.SessionSteps, error) {
	if sessions <= 0 {
		return nil, nil
	}
	if sessions > recentStepSessionsMax {
		sessions = recentStepSessionsMax
	}
	metas, err := a.store.ListSessions(ctx, workdir)
	if err != nil {
		return nil, fmt.Errorf("recent steps: %w", err)
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	var out []port.SessionSteps
	for _, m := range metas {
		if len(out) == sessions {
			break
		}
		if m.Workdir != "" && m.Workdir != workdir {
			continue
		}
		evs, err := a.store.Read(ctx, m.ID, 0)
		if err != nil {
			continue
		}
		steps := stepsOfMode(evs, false, true)
		if len(steps) > recentStepsPerSession {
			steps = steps[len(steps)-recentStepsPerSession:]
		}
		out = append(out, port.SessionSteps{Session: string(m.ID), Steps: steps})
	}
	return out, nil
}
