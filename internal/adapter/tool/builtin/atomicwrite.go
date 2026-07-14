package builtin

import (
	"os"
	"path/filepath"
)

// atomicWriteFile replaces path's contents via a same-directory temp file and
// rename, so a crash or kill mid-write leaves either the old file or the new
// one — never a truncated hybrid. Overnight autonomous runs get killed at
// arbitrary points; a plain os.WriteFile there can destroy the very file the
// edit meant to improve, and the risk window grows with file size.
//
// An existing file keeps its permission bits; a new file gets perm. A symlink is
// followed so the pointed-to file is replaced (as os.WriteFile would), never turned
// into a regular file — otherwise editing a symlinked source would silently sever
// the link.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Resolve a symlink to its target so the temp+rename swaps the real file, not
	// the link. EvalSymlinks needs the target to exist; a dangling or non-symlink
	// path falls through to the literal path unchanged.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
	}
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Any failure past this point must not leave the temp file behind.
	fail := func(e error) error {
		tmp.Close()
		os.Remove(tmpName)
		return e
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
