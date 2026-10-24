package app

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// File checkpoints capture only the chosen resource, never its neighboring files.
// Private directories still contain execution scratch and verification artifacts.
func (w *checkpointWorkspace) files(root string) (map[string]checkpointFile, error) {
	if w.kind != "file" {
		return checkpointFiles(root)
	}
	rel := w.target
	if !filepath.IsLocal(rel) || rel == "." || strings.ContainsAny(rel, "\x00") {
		return nil, fmt.Errorf("checkpoint: file requires a local relative path")
	}
	for path := rel; path != "."; path = filepath.Dir(path) {
		if filepath.Base(path) == ".git" {
			return nil, fmt.Errorf("checkpoint: Git metadata cannot be a file resource")
		}
		st, err := os.Lstat(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("checkpoint: file resource cannot traverse a symlink")
		}
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("checkpoint: resource must be a regular file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return map[string]checkpointFile{rel: {Digest: fmt.Sprintf("%x", h.Sum(nil)), Mode: st.Mode().Perm()}}, nil
}
func (w *checkpointWorkspace) copy(source, target string) error {
	if w.kind != "file" {
		return checkpointCopy(source, target)
	}
	files, err := w.files(source)
	if err != nil {
		return err
	}
	return checkpointInstall(source, target, w.target, files[w.target].Mode)
}

func (w *checkpointWorkspace) prepareFileVerifier(branch, path string) error {
	if w.kind != "file" || path == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.branches[branch]
	if b == nil {
		return fmt.Errorf("checkpoint: unknown verifier branch")
	}
	if filepath.Clean(path) == filepath.Clean(w.target) {
		return fmt.Errorf("checkpoint: verifier must be separate from the document resource")
	}
	if w.verifiers == nil {
		w.verifiers = map[string][]byte{}
	}
	body, exists := w.verifiers[path]
	if !exists {
		v, err := readTrialVerifier(w.original, path)
		if err != nil {
			return err
		}
		body = v.body
		w.verifiers[path] = body
		if err := w.save(); err != nil {
			return err
		}
	}
	r, err := os.OpenRoot(b.Path)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := r.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
