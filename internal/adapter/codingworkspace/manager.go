// Package codingworkspace implements isolated Git/copy workspaces and guarded
// promotion of copy-workspace changes. It never merges or pushes automatically.
package codingworkspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"github.com/sayaya1090/magi/internal/filelock"
	"github.com/sayaya1090/magi/internal/quietconsole"
)

type Manager struct {
	Root           string
	MaxFiles       int
	MaxBytes       int64
	MaxStoredBytes int64
}

func New(root string) *Manager {
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	root = resolveExistingAncestor(root)
	return &Manager{Root: root, MaxFiles: 5000, MaxBytes: 128 << 20, MaxStoredBytes: 512 << 20}
}

// Resolve existing parents too: macOS /var and /tmp aliases must not change
// persisted task identity when the storage directory is first created.
func resolveExistingAncestor(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveExistingAncestor(parent), filepath.Base(path))
}

var safeID = regexp.MustCompile(`^[a-f0-9]{24}$`)

func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func nonce() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func (m *Manager) dir(id string) (string, error) {
	if !safeID.MatchString(id) {
		return "", fmt.Errorf("invalid coding task ID")
	}
	return filepath.Join(m.Root, "tasks", id), nil
}
func save(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o600)
}
func (m *Manager) write(r *task.Record) error {
	r.Updated = time.Now().UTC()
	dir, err := m.dir(r.ID)
	if err != nil {
		return err
	}
	return save(filepath.Join(dir, "task.json"), r)
}
func (m *Manager) lock(ctx context.Context, id string) (func(), error) {
	dir, err := m.dir(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(dir, "task.lock"))
}

func (m *Manager) Status(ctx context.Context, source, id string) (task.Record, error) {
	var r task.Record
	root, err := canonical(source)
	if err != nil {
		return r, err
	}
	dir, err := m.dir(id)
	if err != nil {
		return r, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("coding task record is invalid: %w", err)
	}
	if r.Version != 1 || r.ID != id || r.Workspace != filepath.Join(dir, "workspace") {
		return task.Record{}, fmt.Errorf("coding task identity/workspace mismatch")
	}
	if r.Source != root {
		return task.Record{}, fmt.Errorf("task belongs to source %s; use that source session (or CLI --source). The isolated task directory is not the original source, and prepare never switches this session", r.Source)
	}
	for path, f := range r.Baseline {
		if !validRelative(path) {
			return task.Record{}, fmt.Errorf("invalid manifest path")
		}
		if err := validateFile(&f); err != nil {
			return task.Record{}, err
		}
	}
	return r, nil
}
func (m *Manager) List(ctx context.Context, source string) ([]task.Record, error) {
	entries, err := os.ReadDir(filepath.Join(m.Root, "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return []task.Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	root, err := canonical(source)
	if err != nil {
		return nil, err
	}
	out := []task.Record{}
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		dir, _ := m.dir(e.Name())
		b, readErr := os.ReadFile(filepath.Join(dir, "task.json"))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		} // lease directory before record creation
		if readErr != nil {
			return out, readErr
		}
		var candidate task.Record
		if err := json.Unmarshal(b, &candidate); err != nil {
			return out, fmt.Errorf("invalid coding task record %s: %w", e.Name(), err)
		}
		if candidate.Source != root {
			continue
		}
		r, err := m.Status(ctx, root, e.Name())
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = []string{}
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GIT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	quietconsole.Apply(cmd)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	b := output.Bytes()
	if output.overflow {
		return "", fmt.Errorf("git output exceeded 1 MiB; workspace preserved")
	}
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", args[0], err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}
func detectGit(ctx context.Context, root string) (bool, error) {
	marker := false
	for dir := root; ; dir = filepath.Dir(dir) {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			marker = true
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		if marker {
			return false, fmt.Errorf("repository found but git is not installed")
		}
		return false, nil
	}
	out, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if marker {
			return false, err
		}
		return false, nil
	}
	if out != "true" {
		return false, fmt.Errorf("not a working tree; bare repositories are unsupported")
	}
	return true, nil
}

func (m *Manager) Prepare(ctx context.Context, source string, q task.Request) (r task.Record, err error) {
	root, err := canonical(source)
	if err != nil {
		return r, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return r, fmt.Errorf("coding task source must be a directory")
	}
	store, err := filepath.Abs(resolveExistingAncestor(m.Root))
	if err != nil {
		return r, err
	}
	if inside(root, store) || inside(store, root) {
		return r, fmt.Errorf("task storage and source must not contain each other")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if q.RequestID == "" {
		q.RequestID = nonce()
	}
	if len(q.RequestID) > 256 {
		return r, fmt.Errorf("request ID too long")
	}
	if q.Mode == "" {
		q.Mode = "auto"
	}
	if q.Mode != "auto" && q.Mode != "git" && q.Mode != "copy" {
		return r, fmt.Errorf("unsupported isolation mode; direct editing is not implemented")
	}
	spec, _ := json.Marshal(struct{ Root, Request, Purpose, Mode, Base string }{root, q.RequestID, q.Purpose, q.Mode, q.Base})
	id := hash([]byte(root + "\x00" + q.RequestID))[:24]
	release, err := m.lock(ctx, id)
	if err != nil {
		return r, err
	}
	defer release()
	dir, _ := m.dir(id)
	if existing, e := m.Status(ctx, root, id); e == nil {
		if existing.RequestDigest != hash(spec) {
			return r, fmt.Errorf("request ID reused with different parameters")
		}
		if existing.State == "ready" {
			return existing, nil
		}
		return existing, fmt.Errorf("task requires recovery; existing workspace has been preserved: %s", existing.Error)
	} else if !errors.Is(e, os.ErrNotExist) {
		return r, e
	}
	var hasGit bool
	if q.Mode != "copy" {
		hasGit, err = detectGit(ctx, root)
		if err != nil {
			return r, err
		}
	}
	mode := q.Mode
	if mode == "auto" {
		mode = "copy"
		if hasGit {
			mode = "git"
		}
	}
	if mode == "copy" && q.Base != "" {
		return r, fmt.Errorf("base is only valid for Git tasks; copy mode has no commit base")
	}
	if mode == "git" && !hasGit {
		return r, fmt.Errorf("git mode requested but no repository is available")
	}
	r = task.Record{Version: 1, ID: id, RequestID: q.RequestID, RequestDigest: hash(spec), Purpose: q.Purpose, Source: root, Workspace: filepath.Join(dir, "workspace"), Mode: mode, State: "preparing", Created: time.Now().UTC()}
	if err := m.write(&r); err != nil {
		return r, err
	}
	defer func() {
		if err != nil {
			r.State = "recovery-required"
			r.Error = err.Error()
			if saveErr := m.write(&r); saveErr != nil {
				err = errors.Join(err, fmt.Errorf("could not persist recovery state: %w", saveErr))
			}
		}
	}()
	if mode == "git" {
		top, e := git(ctx, root, "rev-parse", "--show-toplevel")
		if e != nil {
			return r, e
		}
		top, e = canonical(top)
		if e != nil {
			return r, e
		}
		if top != root {
			return r, fmt.Errorf("Git task source must be repository root %s; select copy mode for a subdirectory", top)
		}
		base := q.Base
		if base == "" {
			base = "HEAD"
		}
		r.Base, err = git(ctx, root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
		if err != nil {
			return r, fmt.Errorf("repository has no usable base commit; explicitly choose copy mode or prepare a commit: %w", err)
		}
		dirty, e := git(ctx, root, "status", "--porcelain")
		if e != nil {
			return r, e
		}
		if dirty != "" && q.Base == "" {
			return r, fmt.Errorf("source has uncommitted changes; choose an explicit base (excluded from task) or copy mode")
		}
		r.CommonDir, err = git(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return r, err
		}
		if err = os.MkdirAll(filepath.Join(m.Root, "locks"), 0o700); err != nil {
			return r, err
		}
		unlock, e := filelock.Acquire(ctx, filepath.Join(m.Root, "locks", hash([]byte(r.CommonDir))+".lock"))
		if e != nil {
			return r, e
		}
		defer unlock()
		r.Branch = "magi/task-" + id
		if err = m.write(&r); err != nil {
			return r, err
		}
		_, err = git(ctx, root, "worktree", "add", "-b", r.Branch, "--", r.Workspace, r.Base)
		if err != nil {
			return r, err
		}
	} else if err = os.MkdirAll(r.Workspace, 0o700); err != nil {
		return r, err
	}
	baselineRoot := r.Workspace
	if mode == "copy" {
		baselineRoot = root
	}
	r.Baseline, r.Excluded, err = m.snapshot(ctx, dir, baselineRoot)
	if err != nil {
		return r, err
	}
	if mode == "copy" {
		for path, f := range r.Baseline {
			if err = m.writeFile(dir, r.Workspace, path, &f); err != nil {
				return r, err
			}
		}
	}
	r.State = "ready"
	err = m.write(&r)
	return r, err
}

// Drain excess output without retaining it so a noisy hook cannot exhaust
// memory or block Git on a full pipe. Never parse a truncated answer.
type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, err := b.Buffer.Write(p)
	return n, err
}

// Reject a task belonging to another source before waiting on its writer lease.
// Otherwise a model running inside its task workspace could wait on its own
// execution lease using the original task ID and never finish the turn.
func (m *Manager) lockExisting(ctx context.Context, source, id string) (func(), error) {
	if _, err := m.Status(ctx, source, id); err != nil {
		return nil, err
	}
	return m.lock(ctx, id)
}
