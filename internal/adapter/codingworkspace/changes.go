package codingworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"github.com/sayaya1090/magi/internal/filelock"
)

func different(a, b *task.File) bool {
	if a == nil || b == nil {
		return a != b
	}
	return *a != *b
}
func at(m task.Manifest, path string) *task.File {
	f, ok := m[path]
	if !ok {
		return nil
	}
	return &f
}
func changes(before, after task.Manifest) []task.Change {
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	var names []string
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	out := []task.Change{}
	for _, path := range names {
		a, b := at(before, path), at(after, path)
		if !different(a, b) {
			continue
		}
		kind := "modify"
		if a == nil {
			kind = "create"
		} else if b == nil {
			kind = "delete"
		}
		out = append(out, task.Change{Path: path, Kind: kind, Before: a, After: b})
	}
	return out
}
func (m *Manager) Diff(ctx context.Context, source, id string) ([]task.Change, error) {
	release, err := m.lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return nil, err
	}
	if r.State != "ready" {
		return nil, fmt.Errorf("task is not ready: %s", r.State)
	}
	dir, _ := m.dir(id)
	current, _, err := m.snapshot(ctx, dir, r.Workspace)
	if err != nil {
		return nil, err
	}
	return changes(r.Baseline, current), nil
}
func (m *Manager) sourceLock(ctx context.Context, source string) (func(), error) {
	dir := filepath.Join(m.Root, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(dir, hash([]byte(source))+".source.lock"))
}
func (m *Manager) current(ctx context.Context, root, path string) (*task.File, error) {
	f, err := m.readFile(ctx, root, path, m.MaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Apply is explicitly selected file promotion for copy mode. Git integration
// uses reviewed commits/PRs instead and is never silently replaced by file copy.
func (m *Manager) Apply(ctx context.Context, source, id string, paths []string) (task.Journal, error) {
	var empty task.Journal
	if len(paths) == 0 {
		return empty, fmt.Errorf("select paths to apply after reviewing the task diff")
	}
	release, err := m.lock(ctx, id)
	if err != nil {
		return empty, err
	}
	defer release()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return empty, err
	}
	if r.State != "ready" || r.Mode != "copy" {
		return empty, fmt.Errorf("file promotion requires a ready copy task; Git changes require commit/PR review")
	}
	unlock, err := m.sourceLock(ctx, r.Source)
	if err != nil {
		return empty, err
	}
	defer unlock()
	dir, _ := m.dir(id)
	journalPath := filepath.Join(dir, "integration.json")
	if b, e := os.ReadFile(journalPath); e == nil {
		var existing task.Journal
		if json.Unmarshal(b, &existing) != nil {
			return empty, fmt.Errorf("invalid integration journal")
		}
		return existing, fmt.Errorf("previous integration exists; inspect/restore it before applying again")
	} else if !os.IsNotExist(e) {
		return empty, e
	}
	workspace, _, err := m.snapshot(ctx, dir, r.Workspace)
	if err != nil {
		return empty, err
	}
	all := changes(r.Baseline, workspace)
	byPath := map[string]task.Change{}
	for _, change := range all {
		byPath[change.Path] = change
	}
	journal := task.Journal{ID: nonce(), TaskID: id, Kind: "apply", State: "prepared", Outcomes: []task.Outcome{}}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		change, ok := byPath[path]
		if !ok {
			return empty, fmt.Errorf("path is not a task change: %s", path)
		}
		current, err := m.current(ctx, r.Source, path)
		if err != nil {
			return empty, err
		}
		if different(current, change.Before) && different(current, change.After) {
			journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: path, State: "conflict", Reason: "source changed since preparation"})
			continue
		}
		if !different(current, change.After) {
			journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: path, State: "unchanged"})
			continue
		}
		journal.Changes = append(journal.Changes, change)
	}
	// Baseline blobs were saved before preparation, workspace blobs before this
	// intent. Refuse a missing backup before changing any source file.
	for _, change := range journal.Changes {
		for _, f := range []*task.File{change.Before, change.After} {
			if f != nil {
				if err := validateFile(f); err != nil {
					return empty, err
				}
				b, err := os.ReadFile(filepath.Join(dir, "blobs", f.Digest))
				if err != nil || hash(b) != f.Digest {
					return empty, fmt.Errorf("backup unavailable for %s", change.Path)
				}
			}
		}
	}
	if err := save(journalPath, journal); err != nil {
		return empty, err
	}
	for _, change := range journal.Changes {
		if err := ctx.Err(); err != nil {
			journal.State = "uncertain"
			_ = save(journalPath, journal)
			return journal, err
		}
		current, e := m.current(ctx, r.Source, change.Path)
		if e != nil {
			journal.State = "uncertain"
			journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: change.Path, State: "uncertain", Reason: e.Error()})
			if saveErr := save(journalPath, journal); saveErr != nil {
				return journal, errors.Join(e, saveErr)
			}
			return journal, e
		}
		if different(current, change.Before) {
			journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: change.Path, State: "conflict", Reason: "source changed before write"})
			continue
		}
		if err := m.writeFile(dir, r.Source, change.Path, change.After); err != nil {
			journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: change.Path, State: "uncertain", Reason: err.Error()})
			journal.State = "uncertain"
			_ = save(journalPath, journal)
			return journal, err
		}
		journal.Outcomes = append(journal.Outcomes, task.Outcome{Path: change.Path, State: "applied"})
		if err := save(journalPath, journal); err != nil {
			return journal, fmt.Errorf("source changed but journal update failed: %w", err)
		}
	}
	journal.State = "applied"
	for _, outcome := range journal.Outcomes {
		if outcome.State == "conflict" {
			journal.State = "partial"
		}
	}
	err = save(journalPath, journal)
	return journal, err
}

// Restore never overwrites a later user edit. It rechecks the observed after
// state even when an interrupted apply never recorded its final outcome.
func (m *Manager) Restore(ctx context.Context, source, id string) (task.Journal, error) {
	var j task.Journal
	release, err := m.lock(ctx, id)
	if err != nil {
		return j, err
	}
	defer release()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return j, err
	}
	unlock, err := m.sourceLock(ctx, r.Source)
	if err != nil {
		return j, err
	}
	defer unlock()
	dir, _ := m.dir(id)
	path := filepath.Join(dir, "integration.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return j, err
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return j, err
	}
	if j.TaskID != id || j.Kind != "apply" || !safeID.MatchString(j.ID) {
		return j, fmt.Errorf("integration task mismatch")
	}
	seen := map[string]bool{}
	for _, change := range j.Changes {
		if seen[change.Path] {
			return j, fmt.Errorf("duplicate integration path")
		}
		seen[change.Path] = true
		if change.Before == nil && change.After == nil {
			return j, fmt.Errorf("empty integration change")
		}
		if _, err := target(r.Source, change.Path); err != nil {
			return j, err
		}
		if err := validateFile(change.Before); err != nil {
			return j, err
		}
		if err := validateFile(change.After); err != nil {
			return j, err
		}
	}
	if j.State == "restored" {
		return j, nil
	}
	if j.ApplyState == "" {
		j.ApplyState = j.State
		j.ApplyOutcomes = append([]task.Outcome(nil), j.Outcomes...)
	}
	j.State = "restoring"
	j.Outcomes = []task.Outcome{}
	if err := save(path, j); err != nil {
		return j, err
	}
	for i := len(j.Changes) - 1; i >= 0; i-- {
		change := j.Changes[i]
		current, err := m.current(ctx, r.Source, change.Path)
		out := task.Outcome{Path: change.Path}
		switch {
		case err != nil:
			out.State = "uncertain"
			out.Reason = err.Error()
		case !different(current, change.Before):
			out.State = "unchanged"
		case different(current, change.After):
			out.State = "conflict"
			out.Reason = "file changed after task integration; current contents preserved"
		default:
			if err := ctx.Err(); err != nil {
				return j, err
			}
			if err := m.writeFile(dir, r.Source, change.Path, change.Before); err != nil {
				out.State = "uncertain"
				out.Reason = err.Error()
			} else {
				out.State = "restored"
			}
		}
		j.Outcomes = append(j.Outcomes, out)
		if err := save(path, j); err != nil {
			return j, err
		}
	}
	j.State = "restored"
	for _, out := range j.Outcomes {
		if out.State == "conflict" || out.State == "uncertain" {
			j.State = "partial"
		}
	}
	return j, save(path, j)
}

// Run holds a per-task writer lease throughout the supplied execution. A crash
// releases the kernel lock; stale 'running' state is exposed as recovery-needed.
func (m *Manager) Run(ctx context.Context, source, id string, run func(context.Context, string) error) (task.Record, error) {
	release, err := m.lock(ctx, id)
	if err != nil {
		return task.Record{}, err
	}
	defer release()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return r, err
	}
	if r.State != "ready" && r.State != "stopped" {
		return r, fmt.Errorf("task is not ready; state=%s", r.State)
	}
	r.State = "running"
	if err := m.write(&r); err != nil {
		return r, err
	}
	err = run(ctx, r.Workspace)
	r.State = "ready"
	r.Error = ""
	if err != nil {
		r.State = "stopped"
		r.Error = err.Error()
	}
	if saveErr := m.write(&r); saveErr != nil {
		return r, fmt.Errorf("execution ended; state write failed: %w", saveErr)
	}
	return r, err
}
