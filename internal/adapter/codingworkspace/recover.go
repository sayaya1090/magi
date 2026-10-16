package codingworkspace

import (
	"context"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
)

// Recover only reopens a complete workspace after acquiring its writer lease.
// A partially prepared workspace has no trustworthy baseline and is preserved
// for inspection rather than silently blessing its contents as a new baseline.
func (m *Manager) Recover(ctx context.Context, source, id string) (task.Record, error) {
	unlock, err := m.lockExisting(ctx, source, id)
	if err != nil {
		return task.Record{}, err
	}
	defer unlock()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return r, err
	}
	if r.State == "ready" {
		return r, nil
	}
	if r.State != "running" && r.State != "stopped" {
		return r, fmt.Errorf("incomplete preparation preserved at %s; inspect it and prepare a new request ID", r.Workspace)
	}
	dir, _ := m.dir(id)
	if _, _, err = m.snapshot(ctx, dir, r.Workspace, r.Baseline); err != nil {
		return r, err
	}
	if r.Mode == "git" {
		branch, e := git(ctx, r.Workspace, "symbolic-ref", "--short", "HEAD")
		if e != nil {
			return r, e
		}
		if branch != r.Branch {
			return r, fmt.Errorf("task branch changed; preserved for inspection")
		}
	}
	r.State = "ready"
	r.Error = ""
	err = m.write(&r)
	return r, err
}
