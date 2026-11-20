//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// holdTUI marks a standalone terminal UI as alive in this workspace for as long as it runs: a shared
// flock on <socket>.tui.lock, which the kernel drops however the process dies — SIGKILL included.
// sole reports that this process took it EXCLUSIVELY first, i.e. no other terminal here is alive,
// so recovery cannot close a turn another one is running. share downgrades to the shared hold once
// that is done; release ends it.
//
// A terminal that is not alone starts as if it were not — it recovers nothing and opens a new
// conversation, as every start did before. It still takes its shared place: when another holds the
// hold exclusively (recovering) it waits for it, however long, saying so every tuiShareWait — a
// terminal that ran unheld would be invisible to the next one once the first is gone.
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
	// Another terminal is alive (or recovering for a moment): hold shared, recover nothing.
	deadline := time.Now().Add(tuiShareWait)
	for {
		err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			return release, false, noop
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "magi: another terminal here is still recovering (%v); waiting for it to finish — Ctrl-C to stop\n", err)
			deadline = time.Now().Add(tuiShareWait)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tuiShareWait is how often a terminal waiting for another's recovery says it is still waiting.
// Recovery holds the hold exclusively only while it closes what a stop left open.
var tuiShareWait = 10 * time.Second
