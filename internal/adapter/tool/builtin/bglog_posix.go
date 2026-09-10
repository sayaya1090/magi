//go:build !windows

package builtin

import "os"

// openBackgroundLog opens the file a background command's output is written to.
//
// O_APPEND, so that when rotateIfHuge truncates the file to bound disk, the child's next write
// lands cleanly at offset 0 — append seeks to EOF on every write — instead of at the fd's old,
// now-past-EOF offset, which would leave a sparse hole full of NUL bytes in front of it.
func openBackgroundLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
}
