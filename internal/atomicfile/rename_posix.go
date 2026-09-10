//go:build !windows

package atomicfile

import "os"

// Replace puts the temp file in place of the destination.
//
// One call, because that is all it takes: rename over an open file is defined here, and a reader
// holding the old inode keeps reading it until it closes.
func Replace(from, to string) error { return os.Rename(from, to) }

// ReadFile reads a file that Write may be replacing under it.
//
// Plain os.ReadFile here: a rename is atomic, so a reader gets the whole old file or the whole new
// one and never a failure in between.
func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
