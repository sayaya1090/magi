//go:build darwin || linux

package jsonl

import (
	"fmt"
	"os"
	"syscall"
)

// Include ctime: edits that restore mtime and size must invalidate saved metadata too.
func listingIdentity(info os.FileInfo) string {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", st.Dev, st.Ino, listingChangeTime(st))
}
