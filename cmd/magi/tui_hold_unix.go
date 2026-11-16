//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// holdTUI marks a standalone terminal UI as alive in this workspace for as long as it runs: a shared
// flock on <socket>.tui.lock, which the kernel drops however the process dies — SIGKILL included.
// sole reports that this process took it EXCLUSIVELY first, i.e. no other terminal here is alive,
// so recovery cannot close a turn another one is running. share downgrades to the shared hold once
// that is done; release ends it.
//
// Nothing here blocks: a terminal that cannot tell whether it is alone starts as if it were not —
// it recovers nothing and opens a new conversation, as every start did before.
func holdTUI(sock string) (release func(), sole bool, share func()) {
	noop := func() {}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return noop, false, noop
	}
	f, err := os.OpenFile(sock+".tui.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, false, noop
	}
	fd := int(f.Fd())
	release = func() { f.Close() }
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) == nil {
		return release, true, func() {
			if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
				// Still held exclusively: a terminal started meanwhile finds itself not alone and
				// recovers nothing, which is the safe side.
				fmt.Fprintf(os.Stderr, "magi: terminal hold stays exclusive: %v\n", err)
			}
		}
	}
	// Another terminal is alive (or recovering for a moment): hold shared if we can, recover nothing.
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		f.Close()
		return noop, false, noop
	}
	return release, false, noop
}
