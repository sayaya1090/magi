package builtin

import (
	"os"

	"github.com/sayaya1090/magi/internal/atomicfile"
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
	return atomicfile.Write(path, data, perm)
}
