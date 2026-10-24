package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// checkpointWorkspace belongs to one parent session. Models receive branch IDs,
// never a way to designate arbitrary writable directories. The manifest survives
// cancellation and restart; files are retained until verified publication.
type checkpointWorkspace struct {
	requiresDocuments                                     bool
	documents                                             port.CheckpointDocuments
	handles                                               map[string]port.CheckpointDocument
	context                                               []session.Message
	contextSource                                         string
	contextDigest                                         string
	contextSystem                                         string
	verifiers                                             map[string][]byte
	mu                                                    sync.Mutex
	root, original, kind, current, source, memory, target string
	baseline, initial                                     map[string]checkpointFile
	branches                                              map[string]*checkpointBranch
	history                                               []port.CheckpointSelection
	ready                                                 bool
	phase                                                 string
}
type checkpointFile struct {
	Digest string
	Mode   fs.FileMode
}
type checkpointBranch struct {
	Context        []session.Message
	Session        string
	ID, Path, Base string
	Busy, Sealed   bool
	GitFile        string
}
type checkpointManifest struct {
	RequiresDocuments                                      bool
	Context                                                []session.Message
	ContextSystem                                          string
	ContextSource, ContextDigest                           string
	Verifiers                                              map[string][]byte
	Original, Kind, Current, Source, Phase, Memory, Target string
	Baseline, Initial                                      map[string]checkpointFile
	Branches                                               map[string]*checkpointBranch
	History                                                []port.CheckpointSelection
	Ready                                                  bool
}

func checkpointFiles(root string) (map[string]checkpointFile, error) {
	files := map[string]checkpointFile{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Links could reach the original, another branch, or shared app storage.
		if !info.Mode().IsRegular() {
			return fmt.Errorf("checkpoint: unsupported non-regular file %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = checkpointFile{fmt.Sprintf("%x", h.Sum(nil)), info.Mode().Perm()}
		return nil
	})
	return files, err
}
func checkpointCopy(src, dst string) error {
	files, err := checkpointFiles(src)
	if err != nil {
		return err
	}
	for rel, f := range files {
		if err := checkpointCopyFile(filepath.Join(src, rel), filepath.Join(dst, rel), f.Mode); err != nil {
			return err
		}
	}
	return nil
}
func checkpointCopyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".magi-copy-*")
	if err != nil {
		return err
	}
	temp := out.Name()
	defer os.Remove(temp)
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(mode)
	}
	if err == nil {
		err = out.Sync()
	}
	ce := out.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, dst)
}
func (w *checkpointWorkspace) save() error {
	m := checkpointManifest{RequiresDocuments: w.requiresDocuments, ContextSystem: w.contextSystem, Context: w.context, ContextSource: w.contextSource, ContextDigest: w.contextDigest, Original: w.original, Kind: w.kind, Current: w.current, Source: w.source, Phase: w.phase, Memory: w.memory, Target: w.target, Baseline: w.baseline, Initial: w.initial, Branches: w.branches, History: w.history, Ready: w.ready, Verifiers: w.verifiers}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(w.root, "manifest.json")
	f, err := os.CreateTemp(w.root, "manifest-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func (a *App) checkpointFor(ctx context.Context, sdir, owner, kind string, targets ...string) (*checkpointWorkspace, error) {
	// Repository capture can be slow; never hold the global App lock over I/O.
	a.mu.Lock()
	st := a.stateLocked(session.SessionID(owner))
	a.mu.Unlock()
	st.checkpointInit.Lock()
	defer st.checkpointInit.Unlock()
	a.mu.Lock()
	existing := st.checkpoint
	a.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	var root string
	if a.cfg.StoreDir != "" {
		root = filepath.Join(a.cfg.StoreDir, "checkpoints", owner)
		if rel, err := filepath.Rel(sdir, root); err == nil && filepath.IsLocal(rel) {
			return nil, fmt.Errorf("checkpoint: state directory must be outside the original workspace")
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
	} else {
		var err error
		root, err = os.MkdirTemp("", "magi-checkpoint-*")
		if err != nil {
			return nil, err
		}
	}
	target := ""
	if len(targets) > 0 {
		target = targets[0]
	}
	w := &checkpointWorkspace{requiresDocuments: a.cfg.CheckpointDocuments != nil, documents: a.cfg.CheckpointDocuments, handles: map[string]port.CheckpointDocument{}, target: target, root: root, original: sdir, kind: kind, branches: map[string]*checkpointBranch{}, phase: "open"}
	if b, err := os.ReadFile(filepath.Join(root, "manifest.json")); err == nil {
		var m checkpointManifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		if m.Baseline == nil || m.Initial == nil || m.Branches == nil || (m.Kind != "git" && m.Kind != "directory" && m.Kind != "file") {
			return nil, fmt.Errorf("checkpoint: incomplete manifest; retained at %s", root)
		}
		if m.Original != sdir {
			return nil, fmt.Errorf("checkpoint: original workspace mismatch")
		}
		w.original, w.kind, w.current, w.source, w.phase, w.memory = m.Original, m.Kind, m.Current, m.Source, m.Phase, m.Memory
		w.requiresDocuments = m.RequiresDocuments
		w.target = m.Target
		w.verifiers = m.Verifiers
		w.context, w.contextSource, w.contextDigest = m.Context, m.ContextSource, m.ContextDigest
		w.contextSystem = m.ContextSystem
		w.baseline, w.initial, w.branches, w.history, w.ready = m.Baseline, m.Initial, m.Branches, m.History, m.Ready
		// No execution can survive daemon shutdown. Busy records remain inspectable,
		// but interrupted branches are sealed; fork again rather than adopting partial work.
		for _, b := range w.branches {
			if b.Busy {
				b.Sealed = true
			}
			b.Busy = false
		}
		a.mu.Lock()
		st.checkpoint = w
		a.mu.Unlock()
		return w, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if kind == "" {
		kind = "directory"
		if gitAvailable(sdir) {
			kind = "git"
		}
		w.kind = kind
	}
	if kind != "git" && kind != "directory" && kind != "file" {
		return nil, fmt.Errorf("checkpoint: kind must be git, directory or file")
	}
	before, err := w.files(sdir)
	if err != nil {
		return nil, err
	}
	w.baseline = before
	w.memory = a.projectMemory(sdir)
	if kind == "git" {
		source, _, err := a.cloneWorkspace(ctx, sdir)
		if err != nil {
			return nil, err
		}
		// Keep the private repository beneath the durable manifest, so all worktrees
		// share private metadata rather than the user's index and refs.
		defer os.RemoveAll(filepath.Dir(source))
		w.source = filepath.Join(root, "repository")
		if out, err := gitRun(ctx, sdir, "clone", "--no-hardlinks", "--quiet", source, w.source); err != nil {
			return nil, fmt.Errorf("checkpoint clone: %w: %s", err, out)
		}
		head, err := gitRun(ctx, w.source, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		w.current = strings.TrimSpace(head)
	} else {
		w.current = filepath.Join(root, "snapshot-"+newID())
		if err := os.MkdirAll(w.current, 0700); err != nil {
			return nil, err
		}
		if err := w.copy(sdir, w.current); err != nil {
			return nil, err
		}
	}
	after, err := w.files(sdir)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("checkpoint: original changed while capturing; retry")
	}
	// Compare the actual snapshot as well: ignored Git files must not disappear
	// from publication accounting. Git checkpoints track only Git-visible files.
	if kind == "git" {
		w.initial, err = checkpointFiles(w.source)
	} else {
		w.initial, err = w.files(w.current)
	}
	if err != nil {
		return nil, err
	}
	if err := a.captureCheckpointContext(ctx, w, session.SessionID(owner)); err != nil {
		return nil, err
	}
	if err := w.save(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	st.checkpoint = w
	a.mu.Unlock()
	return w, nil
}

func (w *checkpointWorkspace) info() port.CheckpointResult {
	r := port.CheckpointResult{Kind: w.kind, Checkpoint: fmt.Sprintf("%x", sha256.Sum256([]byte(w.current))), Phase: w.phase, Ready: w.ready, Recovery: w.root, History: w.history, Resource: w.target}
	for _, b := range w.branches {
		resource := ""
		if w.target != "" {
			resource = filepath.Join(b.Path, w.target)
		}
		r.Branches = append(r.Branches, port.CheckpointBranch{Resource: resource, ID: b.ID, Path: b.Path, Checkpoint: b.Base, Busy: b.Busy, Sealed: b.Sealed})
	}
	sort.Slice(r.Branches, func(i, j int) bool { return r.Branches[i].ID < r.Branches[j].ID })
	return r
}
func (w *checkpointWorkspace) action(ctx context.Context, req port.CheckpointRequest) (port.CheckpointResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if (w.phase == "publishing" || w.phase == "recovery_required") && req.Action != "recover" && req.Action != "status" {
		return w.info(), fmt.Errorf("checkpoint: interrupted publication requires recovery; retained at %s", w.root)
	}
	switch req.Action {
	case "recover":
		if err := w.recoverPublication(); err != nil {
			return w.info(), err
		}
	case "begin":
		if w.phase == "complete" || w.phase == "cleanup_pending" {
			if err := w.cleanup(ctx); err != nil {
				return w.info(), err
			}
		}
	case "status":
	case "fork":
		if w.ready || w.phase != "open" {
			return w.info(), fmt.Errorf("checkpoint: tree is not open for forks")
		}
		id := newID()
		path := filepath.Join(w.root, "branch-"+id)
		if w.kind == "git" {
			if out, err := gitRun(ctx, w.source, "worktree", "add", "--detach", path, w.current); err != nil {
				return w.info(), fmt.Errorf("checkpoint fork: %w: %s", err, out)
			}
		} else {
			if err := os.MkdirAll(path, 0700); err != nil {
				return w.info(), err
			}
			if err := w.copy(w.current, path); err != nil {
				return w.info(), err
			}
		}
		b := &checkpointBranch{ID: id, Path: path, Base: w.current}
		if w.kind == "file" && w.documents != nil {
			handle, err := w.documents.OpenCopy(ctx, filepath.Join(path, w.target))
			if err != nil {
				return w.info(), fmt.Errorf("checkpoint document open failed; copy retained at %s: %w", path, err)
			}
			if handle == nil {
				return w.info(), fmt.Errorf("checkpoint document adapter returned no handle")
			}
			w.handles[id] = handle
		}
		if w.kind == "git" {
			body, err := os.ReadFile(filepath.Join(path, ".git"))
			if err != nil {
				return w.info(), err
			}
			b.GitFile = string(body)
		}
		w.branches[id] = b
		if err := w.save(); err != nil {
			return w.info(), err
		}
		r := w.info()
		r.Branch = id
		r.Path = path
		if w.target != "" {
			r.Resource = filepath.Join(path, w.target)
		}
		return r, nil
	case "adopt":
		b := w.branches[req.Branch]
		if b == nil || b.Busy || b.Sealed || b.Base != w.current || w.ready {
			return w.info(), fmt.Errorf("checkpoint: adoption requires an idle, unsealed branch of the current checkpoint")
		}
		if err := w.closeDocument(ctx, b.ID); err != nil {
			return w.info(), err
		}
		if _, err := w.files(b.Path); err != nil {
			return w.info(), err
		}
		if w.kind == "git" {
			body, err := os.ReadFile(filepath.Join(b.Path, ".git"))
			if err != nil || string(body) != b.GitFile {
				return w.info(), fmt.Errorf("checkpoint: branch Git metadata changed")
			}
		}
		previous := w.current
		// Freeze a new checkpoint. Subsequent writes to the old branch cannot change it.
		if w.kind == "git" {
			head, err := commitAll(ctx, b.Path, "magi: adopted checkpoint")
			if err != nil {
				return w.info(), err
			}
			w.current = head
		} else {
			path := filepath.Join(w.root, "snapshot-"+newID())
			if err := os.MkdirAll(path, 0700); err != nil {
				return w.info(), err
			}
			if err := w.copy(b.Path, path); err != nil {
				return w.info(), err
			}
			w.current = path
		}
		b.Sealed = true
		if b.Session != "" {
			w.context = b.Context
			w.contextSource = b.Session
			encoded, err := json.Marshal(w.context)
			if err != nil {
				return w.info(), err
			}
			w.contextDigest = fmt.Sprintf("%x", sha256.Sum256(encoded))
		}
		w.history = append(w.history, port.CheckpointSelection{Branch: b.ID, From: fmt.Sprintf("%x", sha256.Sum256([]byte(previous))), To: fmt.Sprintf("%x", sha256.Sum256([]byte(w.current)))})
	case "finish":
		if len(w.history) == 0 {
			return w.info(), fmt.Errorf("checkpoint: adopt a result before requesting completion")
		}
		for _, b := range w.branches {
			if b.Busy {
				return w.info(), fmt.Errorf("checkpoint: branch is still executing")
			}
		}
		for id := range w.branches {
			if err := w.closeDocument(ctx, id); err != nil {
				return w.info(), err
			}
		}
		w.ready = true // publication is host-owned, after genuine top-level completion
	case "reopen":
		w.ready = false
	default:
		return w.info(), fmt.Errorf("checkpoint: unknown action %q", req.Action)
	}
	if err := w.save(); err != nil {
		return w.info(), err
	}
	return w.info(), nil
}
func (w *checkpointWorkspace) acquire(id string) (string, func(), error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.branches[id]
	if b == nil || b.Busy || b.Sealed || b.Base != w.current || w.ready || w.phase != "open" {
		return "", nil, fmt.Errorf("checkpoint: branch unavailable for writing")
	}
	b.Busy = true
	if err := w.save(); err != nil {
		b.Busy = false
		return "", nil, err
	}
	return b.Path, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		b.Busy = false
		if err := w.save(); err != nil {
			log.Printf("checkpoint manifest persistence: %v", err)
		}
	}, nil
}

// Publication is a journaled file transaction. Original divergence is rejected
// before writes. Each original is backed up; failures roll back or retain the
// journal for recovery. No Git index/refs in the user's repo are changed.
func (w *checkpointWorkspace) publish(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.ready || w.phase == "cleaned" {
		return nil
	}
	if w.phase == "complete" || w.phase == "cleanup_pending" {
		return w.cleanup(ctx)
	}
	if w.phase != "open" {
		return fmt.Errorf("checkpoint: recovery required at %s", w.root)
	}
	for _, b := range w.branches {
		if b.Busy {
			return fmt.Errorf("checkpoint: active branch prevents completion")
		}
	}
	if w.kind == "file" && w.requiresDocuments {
		if w.documents == nil {
			return fmt.Errorf("checkpoint: original application adapter is unavailable; publication refused")
		}
		if err := w.documents.ConfirmClosed(ctx, filepath.Join(w.original, w.target)); err != nil {
			return fmt.Errorf("checkpoint: original document must be closed before publication: %w", err)
		}
	}
	actual, err := w.files(w.original)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, w.baseline) {
		return fmt.Errorf("checkpoint: original changed since capture; retained at %s", w.root)
	}
	selected := w.current
	if w.kind == "git" {
		selected = filepath.Join(w.root, "publish-"+newID())
		if out, err := gitRun(ctx, w.source, "worktree", "add", "--detach", selected, w.current); err != nil {
			return fmt.Errorf("checkpoint final checkout: %w: %s", err, out)
		}
	}
	final, err := w.files(selected)
	if err != nil {
		return err
	}
	desired := map[string]checkpointFile{}
	for p, f := range w.baseline {
		desired[p] = f
	}
	changed := map[string]bool{}
	for p, old := range w.initial {
		if f, ok := final[p]; !ok || f != old {
			changed[p] = true
			delete(desired, p)
		}
	}
	for p, f := range final {
		if old, ok := w.initial[p]; !ok || old != f {
			changed[p] = true
			desired[p] = f
		}
	}
	// Reject file/directory replacements before changing anything. The supported
	// surface is regular-file additions, edits and deletions.
	for p := range changed {
		for d := filepath.Dir(p); d != "."; d = filepath.Dir(d) {
			if _, ok := w.baseline[d]; ok {
				return fmt.Errorf("checkpoint: file/directory replacement requires manual resolution: %s", d)
			}
		}
		if st, err := os.Stat(filepath.Join(w.original, p)); err == nil && st.IsDir() {
			return fmt.Errorf("checkpoint: directory/file replacement requires manual resolution: %s", p)
		}
	}
	backup := filepath.Join(w.root, "backup")
	if err := os.MkdirAll(backup, 0700); err != nil {
		return err
	}
	for p := range changed {
		if f, ok := w.baseline[p]; ok {
			if err := checkpointCopyFile(filepath.Join(w.original, p), filepath.Join(backup, p), f.Mode); err != nil {
				return err
			}
		}
	}
	paths := make([]string, 0, len(changed))
	for p := range changed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// Persist the affected paths before the first mutation for crash recovery.
	journal, err := json.Marshal(struct {
		Paths   []string
		Desired map[string]checkpointFile
	}{paths, desired})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(w.root, "publish-paths.json"), journal, 0600); err != nil {
		return err
	}
	w.phase = "publishing"
	if err := w.save(); err != nil {
		return err
	}
	var applied []string
	rollback := func(cause error) error {
		var failures []string
		observed, scanErr := w.files(w.original)
		if scanErr != nil {
			w.phase = "recovery_required"
			if err := w.save(); err != nil {
				log.Printf("checkpoint recovery persistence: %v", err)
			}
			return fmt.Errorf("%w; rollback requires review: %v", cause, scanErr)
		}
		for _, p := range applied {
			held, exists := observed[p]
			expected, has := desired[p]
			if exists != has || (exists && held != expected) {
				failures = append(failures, "concurrent edit preserved: "+p)
				continue
			}
			var e error
			if f, ok := w.baseline[p]; ok {
				e = checkpointInstall(backup, w.original, p, f.Mode)
			} else {
				e = checkpointRemove(w.original, p)
				if os.IsNotExist(e) {
					e = nil
				}
			}
			if e != nil {
				failures = append(failures, e.Error())
			}
		}
		w.phase = "open"
		if len(failures) > 0 {
			w.phase = "recovery_required"
		}
		if e := w.save(); e != nil {
			failures = append(failures, e.Error())
		}
		return fmt.Errorf("checkpoint publication failed: %w; rollback errors=%v; retained at %s", cause, failures, w.root)
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if f, ok := final[p]; ok {
			err = checkpointInstall(selected, w.original, p, f.Mode)
		} else {
			err = checkpointRemove(w.original, p)
		}
		if err != nil {
			return rollback(err)
		}
		applied = append(applied, p)
	}
	observed, err := w.files(w.original)
	if err != nil {
		return rollback(err)
	}
	if !reflect.DeepEqual(observed, desired) {
		return rollback(fmt.Errorf("post-publication content verification failed"))
	}
	w.phase = "complete"
	if err := w.save(); err != nil {
		return err
	}
	return w.cleanup(ctx)
}
func (w *checkpointWorkspace) cleanup(ctx context.Context) error {
	// Retain the small manifest/history as the audit record; remove all snapshots,
	// worktrees, source repository and backup. Retry safely after partial cleanup.
	for id := range w.branches {
		if err := w.closeDocument(ctx, id); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "manifest.json" || entry.Name() == "context-assets" {
			continue
		}
		if err := ctx.Err(); err != nil {
			w.phase = "cleanup_pending"
			if err := w.save(); err != nil {
				log.Printf("checkpoint manifest persistence: %v", err)
			}
			return err
		}
		if err := os.RemoveAll(filepath.Join(w.root, entry.Name())); err != nil {
			w.phase = "cleanup_pending"
			if err := w.save(); err != nil {
				log.Printf("checkpoint manifest persistence: %v", err)
			}
			return err
		}
	}
	w.phase = "cleaned"
	w.branches = map[string]*checkpointBranch{}
	return w.save()
}

func (a *App) checkpointFn(depth int, s session.Session) func(context.Context, port.CheckpointRequest) (port.CheckpointResult, error) {
	if depth != 0 {
		return nil
	}
	return func(ctx context.Context, req port.CheckpointRequest) (port.CheckpointResult, error) {
		if req.Action != "begin" {
			a.mu.Lock()
			w := a.stateLocked(s.ID).checkpoint
			a.mu.Unlock()
			if w == nil {
				return port.CheckpointResult{}, fmt.Errorf("checkpoint: begin a checkpoint tree first")
			}
			return w.action(ctx, req)
		}
		a.mu.Lock()
		previous := a.stateLocked(s.ID).checkpoint
		a.mu.Unlock()
		if previous != nil {
			previous.mu.Lock()
			cleaned := previous.phase == "cleaned"
			previous.mu.Unlock()
			if cleaned {
				body, err := os.ReadFile(filepath.Join(previous.root, "manifest.json"))
				if err != nil {
					return port.CheckpointResult{}, err
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(previous.root), filepath.Base(previous.root)+"-"+newID()+".json"), body, 0600); err != nil {
					return port.CheckpointResult{}, err
				}
				if err := os.Remove(filepath.Join(previous.root, "manifest.json")); err != nil {
					return port.CheckpointResult{}, err
				}
				a.mu.Lock()
				a.stateLocked(s.ID).checkpoint = nil
				a.mu.Unlock()
			}
		}
		w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), req.Kind, req.Path)
		if err != nil {
			return port.CheckpointResult{}, err
		}
		if req.Path != "" && filepath.Clean(req.Path) != w.target {
			return port.CheckpointResult{}, fmt.Errorf("checkpoint: existing file target differs")
		}
		if req.Kind != "" && req.Kind != w.kind {
			return port.CheckpointResult{}, fmt.Errorf("checkpoint: existing backend differs")
		}
		r, err := w.action(ctx, req)
		if err == nil && r.Phase == "cleaned" {
			return a.checkpointFn(depth, s)(ctx, req)
		}
		return r, err
	}
}
func (a *App) finishCheckpoints(ctx context.Context, sid session.SessionID) error {
	a.mu.Lock()
	w := a.stateLocked(sid).checkpoint
	a.mu.Unlock()
	if w == nil {
		return nil
	}
	w.mu.Lock()
	selectContext := w.ready && w.phase != "cleaned" && len(w.history) > 0
	messages := w.context
	source, digest := w.contextSource, w.contextDigest
	w.mu.Unlock()
	if selectContext && source != "" && source != string(sid) {
		body, err := json.Marshal(event.CheckpointContextData{Messages: messages, SourceSession: source, Digest: digest, Replace: true})
		if err != nil {
			return err
		}
		// The source child log is immutable and remains available after its workspace
		// is disposed. Append once; completion/cleanup retries must not duplicate it.
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			return err
		}
		seen := false
		for _, ev := range evs {
			if ev.Type == event.TypeCheckpointContext {
				var d event.CheckpointContextData
				if json.Unmarshal(ev.Data, &d) == nil && d.Replace && d.Digest == digest {
					seen = true
				}
			}
		}
		if !seen {
			if err := a.appendFact(ctx, sid, event.TypeCheckpointContext, event.Actor{Kind: event.ActorSystem, ID: "checkpoint"}, body); err != nil {
				return err
			}
		}
	}
	if err := w.publish(ctx); err != nil {
		a.appendPart(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "checkpoint"}, "m_"+newID(), session.RoleAssistant, session.Part{Kind: session.PartText, Text: "Checkpoint completion failed; results retained: " + err.Error()})
		return err
	}
	return nil
}

// Recovery refuses files with unexpected concurrent edits, so retrying after a
// crash cannot silently overwrite work performed outside this transaction.
func (w *checkpointWorkspace) recoverPublication() error {
	if w.phase != "publishing" && w.phase != "recovery_required" {
		return fmt.Errorf("checkpoint: no interrupted publication")
	}
	var plan struct {
		Paths   []string
		Desired map[string]checkpointFile
	}
	body, err := os.ReadFile(filepath.Join(w.root, "publish-paths.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &plan); err != nil {
		return err
	}
	actual, err := w.files(w.original)
	if err != nil {
		return err
	}
	for _, p := range plan.Paths {
		held, exists := actual[p]
		before, had := w.baseline[p]
		after, has := plan.Desired[p]
		if !((exists == had && (!exists || held == before)) || (exists == has && (!exists || held == after))) {
			return fmt.Errorf("checkpoint: external edit blocks recovery: %s", p)
		}
	}
	for _, p := range plan.Paths {
		if f, ok := w.baseline[p]; ok {
			if err := checkpointInstall(filepath.Join(w.root, "backup"), w.original, p, f.Mode); err != nil {
				return err
			}
		} else {
			if err := checkpointRemove(w.original, p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	w.phase, w.ready = "open", false
	return w.save()
}

func checkpointRemove(root, rel string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(rel)
}

// Root-relative writes cannot follow a newly introduced symlink out of the
// original workspace while publication or recovery is in progress.
func checkpointInstall(source, target, rel string, mode fs.FileMode) error {
	src, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer src.Close()
	in, err := src.Open(rel)
	if err != nil {
		return err
	}
	defer in.Close()
	dst, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err := dst.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(rel), ".magi-install-"+newID())
	out, err := dst.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if err := dst.Remove(temp); err != nil && !os.IsNotExist(err) {
			log.Printf("checkpoint temporary cleanup: %v", err)
		}
	}()
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(mode)
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return dst.Rename(temp, rel)
}

func (w *checkpointWorkspace) closeDocument(ctx context.Context, id string) error {
	if w.requiresDocuments && w.documents == nil {
		return fmt.Errorf("checkpoint: original application adapter is unavailable; copies retained")
	}
	h := w.handles[id]
	if h == nil {
		if w.documents != nil && w.kind == "file" {
			if b := w.branches[id]; b != nil {
				if err := w.documents.ConfirmClosed(ctx, filepath.Join(b.Path, w.target)); err != nil {
					return fmt.Errorf("checkpoint: document closure not confirmed: %w", err)
				}
			}
		}
		return nil
	}
	if err := h.SaveAndClose(ctx); err != nil {
		return fmt.Errorf("checkpoint: document save/close not confirmed; copy retained: %w", err)
	}
	if w.documents != nil {
		b := w.branches[id]
		if b != nil {
			if err := w.documents.ConfirmClosed(ctx, filepath.Join(b.Path, w.target)); err != nil {
				return fmt.Errorf("checkpoint: document still open after close: %w", err)
			}
		}
	}
	delete(w.handles, id)
	return nil
}
