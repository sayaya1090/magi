package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// recoveryTree keeps the project's directory while walking: a cloned child's Workdir is
// its checkout, but its log (and its descendants) belongs to the original project.
// Children precede parents so a failed child can prevent its parent from closing.
func (a *App) recoveryTree(ctx context.Context, project string, roots []session.SessionMeta, inventory map[session.SessionID][]session.SessionMeta, selectedOnly bool) ([]session.SessionMeta, map[session.SessionID][]session.SessionMeta, map[session.SessionID]error) {
	var ordered []session.SessionMeta
	children := map[session.SessionID][]session.SessionMeta{}
	blocked := map[session.SessionID]error{}
	seen := map[session.SessionID]bool{}
	var visit func(session.SessionMeta)
	visit = func(m session.SessionMeta) {
		if seen[m.ID] {
			return
		}
		seen[m.ID] = true
		if a.runningHere(m.ID) {
			blocked[m.ID] = fmt.Errorf("session %s is still running", m.ID)
			ordered = append(ordered, m)
			return // do not touch the descendants of a live owner
		}
		kids := inventory[m.ID]
		var err error
		if inventory == nil {
			kids, err = a.store.ChildSessions(ctx, project, string(m.ID))
		}
		if err != nil {
			blocked[m.ID] = fmt.Errorf("list children: %w", err)
		}
		if selectedOnly {
			evs, readErr := a.store.Read(ctx, m.ID, 0)
			if readErr != nil {
				blocked[m.ID] = readErr
				kids = nil
			} else {
				_, _, endless := endlessInterruption(evs)
				_, pending := unfinishedTurn(evs)
				if !pending && m.Parent != "" {
					_, pending = unfinishedTurn(withoutErrors(evs))
				}
				if !endless && !pending {
					kids = nil
				} else {
					since := lastPromptAt(evs)
					current := make([]session.SessionMeta, 0, len(kids))
					for _, child := range kids {
						if !child.Created.Before(since) {
							current = append(current, child)
						}
					}
					kids = current
				}
			}
		}
		children[m.ID] = kids
		for _, child := range kids {
			visit(child)
		}
		ordered = append(ordered, m)
	}
	for _, root := range roots {
		visit(root)
	}
	return ordered, children, blocked
}

// Child sessions are one task each. Only those created during the last parent prompt
// belong in its recovery account; old interrupted children must not poison a new turn.
// Read the saved child state, not this pass's return values: a previous recovery may
// have finished the child and died before writing anything to the parent.
func (a *App) recoveryChildren(ctx context.Context, parent []event.Event, children []session.SessionMeta) (string, []session.SessionID, error) {
	since := lastPromptAt(parent)
	var notes []string
	var uncertain []session.SessionID
	for _, child := range children {
		if child.Created.Before(since) {
			continue
		}
		evs, err := a.store.Read(ctx, child.ID, 0)
		if err != nil {
			return "", nil, fmt.Errorf("read child %s: %w", child.ID, err)
		}
		status := "saved child log; inspect its result before repeating its work"
		needsCheck := false
		for _, e := range evs {
			if e.Type == event.TypePromptSubmitted {
				needsCheck = false
				status = "saved child log; inspect its result before repeating its work"
			}
			if e.Type != event.TypeError {
				continue
			}
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil && d.Code == "interrupted" && !d.Recovered {
				status = "interrupted; no work was re-run"
				needsCheck = !d.Resumable
				if !d.Resumable {
					status = "may have run; check its effects before continuing"
				}
			}
		}
		if needsCheck {
			uncertain = append(uncertain, child.ID)
		}
		notes = append(notes, fmt.Sprintf("child %s (workspace %s): %s", child.ID, child.Workdir, status))
	}
	if len(notes) == 0 {
		return "", uncertain, nil
	}
	return "\n\nChild work saved separately:\n" + strings.Join(notes, "\n"), uncertain, nil
}

// lastPromptAt is when the last prompt in a log was submitted; the children created since belong
// to the turn that prompt opened.
func lastPromptAt(evs []event.Event) time.Time {
	var since time.Time
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			since = e.TS
		}
	}
	return since
}

// turnChildrenBlocked joins the failures of the children created during the last turn of parent.
func turnChildrenBlocked(parent []event.Event, kids []session.SessionMeta, blocked map[session.SessionID]error) error {
	since := lastPromptAt(parent)
	var err error
	for _, k := range kids {
		if k.Created.Before(since) {
			continue
		}
		if b := blocked[k.ID]; b != nil {
			err = errors.Join(err, fmt.Errorf("child %s: %w", k.ID, b))
		}
	}
	return err
}

// withoutErrors drops error events. Children run runLoop directly, without startRun's
// terminal-error teardown, so a provider error can stand without turn.finished; read past it to
// settle that child, so nothing waits forever for its terminal event.
func withoutErrors(evs []event.Event) []event.Event {
	out := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		if e.Type != event.TypeError {
			out = append(out, e)
		}
	}
	return out
}
