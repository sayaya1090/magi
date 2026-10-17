//go:build unix

package companionpolicy

import (
	"os"
	"syscall"
)

// Nonblocking open lets us reject a FIFO swapped in after Stat without waiting for a writer.
func openRead(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
