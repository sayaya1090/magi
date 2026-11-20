//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
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
// terminal that ran unheld would be invisible to the next one once the first is gone. A hold that
// can never be had (lockContended says no) is not that: it says why and starts without recovering.
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
	if err = tuiFlock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		return release, true, func() {
			if err := tuiFlock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
				// Still held exclusively: a terminal started meanwhile finds itself not alone and
				// recovers nothing, which is the safe side.
				fmt.Fprintf(tuiStderr, "magi: terminal hold stays exclusive: %v\n", err)
			}
		}
	}
	if !lockContended(err) {
		return cannotHold(f, err)
	}
	// Another terminal is alive (or recovering for a moment): hold shared, recover nothing.
	deadline := time.Now().Add(tuiShareWait)
	for {
		err := tuiFlock(fd, unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			return release, false, noop
		}
		if !lockContended(err) {
			return cannotHold(f, err)
		}
		if time.Now().After(deadline) {
			if lockShortage(err) {
				fmt.Fprintf(tuiStderr, "magi: no file lock can be taken right now (%v); waiting for one — Ctrl-C to stop\n", err)
			} else {
				fmt.Fprintf(tuiStderr, "magi: another terminal here is still recovering (%v); waiting for it to finish — Ctrl-C to stop\n", err)
			}
			deadline = time.Now().Add(tuiShareWait)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tuiShareWait is how often a terminal waiting for another's recovery says it is still waiting.
// Recovery holds the hold exclusively only while it closes what a stop left open.
var tuiShareWait = 10 * time.Second

// tuiFlock and tuiStderr are flock(2) and where notes go; tests stand in for them.
var (
	tuiFlock            = unix.Flock
	tuiStderr io.Writer = os.Stderr
)

// lockContended reports whether a failed flock is one that waiting ends: another terminal holding the
// hold now, a signal cutting the call short, or a shortage of locks (lockShortage). Only a failure that
// no terminal here could ever get past (no flock on this filesystem) is left to cannotHold.
func lockContended(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) || lockShortage(err)
}

// lockShortage reports a flock refused for want of resources (no lock records, no memory). It can pass
// while terminals run, so a terminal that ran unheld through it would be invisible to one that takes
// the lock once it has: it is waited out.
func lockShortage(err error) bool {
	return errors.Is(err, unix.ENOLCK) || errors.Is(err, unix.ENOMEM)
}

// cannotHold is the start of a terminal whose hold can never be taken. No terminal here can take it
// either, so none ever finds itself alone and none recovers: running on is safe, and says so.
func cannotHold(f *os.File, err error) (func(), bool, func()) {
	fmt.Fprintf(tuiStderr, "magi: the terminal hold for this workspace cannot be taken (%v); starting without recovering what a stop left open\n", err)
	f.Close()
	noop := func() {}
	return noop, false, noop
}
