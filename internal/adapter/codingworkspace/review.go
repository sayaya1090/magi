package codingworkspace

import (
	"bytes"
	"context"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// Review returns exact text, without injecting large/binary files into model
// context. Digests remain available when an external editor is needed.
func (m *Manager) Review(ctx context.Context, source, id, path string) (task.Review, error) {
	var out task.Review
	release, err := m.lock(ctx, id)
	if err != nil {
		return out, err
	}
	defer release()
	r, err := m.Status(ctx, source, id)
	if err != nil {
		return out, err
	}
	if r.State != "ready" {
		return out, fmt.Errorf("task is not ready: %s", r.State)
	}
	dir, _ := m.dir(id)
	current, _, err := m.snapshot(ctx, dir, r.Workspace)
	if err != nil {
		return out, err
	}
	found := false
	for _, c := range changes(r.Baseline, current) {
		if c.Path == path {
			out.Change = c
			found = true
			break
		}
	}
	if !found {
		return out, fmt.Errorf("path is not a task change: %s", path)
	}
	for i, f := range []*task.File{out.Before, out.After} {
		if f == nil {
			continue
		}
		if err := validateFile(f); err != nil {
			return out, err
		}
		if f.Size > 256<<10 {
			out.Omitted = "file exceeds 256 KiB text review limit; inspect files in an editor"
			out.BeforeText = nil
			out.AfterText = nil
			return out, nil
		}
		b, err := os.ReadFile(filepath.Join(dir, "blobs", f.Digest))
		if err != nil {
			return out, err
		}
		if hash(b) != f.Digest {
			return out, fmt.Errorf("review blob is corrupt")
		}
		if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			out.Omitted = "binary file; inspect files with a suitable viewer"
			out.BeforeText = nil
			out.AfterText = nil
			return out, nil
		}
		s := string(b)
		if i == 0 {
			out.BeforeText = &s
		} else {
			out.AfterText = &s
		}
	}
	return out, nil
}
