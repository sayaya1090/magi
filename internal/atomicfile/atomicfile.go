// Package atomicfile replaces a file's contents in one step.
//
// os.WriteFile truncates and then writes, and between those two calls the file exists and is empty.
// Whoever else is reading gets the gap: this tree has seen it twice for real — a plugin manifest
// read as "missing name" by a second magi starting at the same moment, and it is the same shape
// that makes an overnight run's kill destroy the file the edit meant to improve.
//
// One implementation, because three copies of a safety property is how two of them drift. Before
// this package there were two — the write tool's and the plugin materialiser's — and the second
// lacked the symlink handling the first had learned.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path's contents via a same-directory temp file and a rename, so a crash, a kill or
// a concurrent reader sees either the old file or the new one and never a hybrid.
//
// An existing file keeps its permission bits; a new one gets perm. A symlink is followed so the
// pointed-to file is replaced, as os.WriteFile would — turning a symlinked source into a regular
// file would silently sever the link.
//
// Same directory on purpose: a rename is only atomic within a filesystem, and a temp file in
// /tmp would degrade to a copy across a mount boundary, which is the very thing being avoided.
func Write(path string, data []byte, perm os.FileMode) error {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		// EvalSymlinks needs the target to exist; a dangling link or a plain path falls through
		// unchanged.
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
	}
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// Any failure past this point must not leave the temp file behind — a directory filling with
	// half-written dot-files is its own defect, and a loader that walks the directory would try to
	// read them.
	fail := func(e error) error {
		tmp.Close()
		os.Remove(name)
		return e
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	// Synced before the rename: the rename is what publishes the file, and publishing a name whose
	// contents are still in a buffer is the hybrid this exists to prevent, one layer down.
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	// CreateTemp makes it 0600; the destination's mode is applied before it takes the name, so it
	// is never briefly readable-by-nobody under the name others use.
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
