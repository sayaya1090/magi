//go:build !unix

package companionpolicy

import "os"

func openRead(root *os.Root, path string) (*os.File, error) { return root.Open(path) }
