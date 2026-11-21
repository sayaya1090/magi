//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// holdTUI marks a standalone terminal UI as alive in this workspace for as long as it runs, and tells
// it whether it is the only one, so recovery never closes a turn another terminal is running.
//
// Two flocks, because one cannot do both jobs. flock(2) turns an exclusive lock into a shared one by
// letting it go and taking it again, so a single lock used both to decide "alone" (exclusive) and to
// mark "alive" (shared) has a gap in which a new terminal finds itself alone.
//
//   - <sock>.tui.lock is the mark: every terminal holds it shared for its whole life. The kernel drops
//     it however the process dies, SIGKILL included.
//   - <sock>.tui.recover.lock is the gate: a terminal holds it exclusively while it decides whether it
//     is alone and, if it is, while it recovers. Terminals take it one at a time, so none can start
//     inside another's handover.
//
// sole reports that this terminal found the mark free, i.e. no other terminal here is alive; by then
// the mark is already held shared. share ends the decision (and recovery) by letting the gate go. release ends the
// hold. A non-nil err means the hold could not be placed and the terminal must not start: running
// unseen would let a later terminal recover a turn this one is in. The one exception is a filesystem
// with no flock at all (classifyLock: lockUnsupported) — no terminal there can hold anything, so none
// is ever alone, none recovers, and starting is safe; it says so.
func holdTUI(sock string) (release func(), sole bool, share func(), err error) {
	noop := func() {}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return noop, false, noop, fmt.Errorf("terminal hold: %w", err)
	}
	gate, err := os.OpenFile(sock+".tui.recover.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, false, noop, fmt.Errorf("terminal hold: %w", err)
	}
	mark, err := os.OpenFile(sock+".tui.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		gate.Close()
		return noop, false, noop, fmt.Errorf("terminal hold: %w", err)
	}
	var gateOnce sync.Once
	letGateGo := func() { gateOnce.Do(func() { gate.Close() }) }
	fail := func(e error) (func(), bool, func(), error) {
		mark.Close()
		letGateGo()
		return noop, false, noop, fmt.Errorf("terminal hold: %w", e)
	}

	// 1. The gate: one terminal at a time decides and recovers.
	if err := waitLock(gate, unix.LOCK_EX, "another terminal here is deciding whether to recover"); err != nil {
		if classifyLock(err) == lockUnsupported {
			fmt.Fprintf(tuiStderr, "magi: this filesystem does not support file locks (%v); no terminal here can tell whether it is alone, so this one starts without recovering what a stop left open\n", err)
			mark.Close()
			letGateGo()
			return noop, false, noop, nil
		}
		return fail(err)
	}
	release = func() {
		mark.Close()
		letGateGo()
	}

	// 2. The mark: free means no other terminal is alive.
	switch err := tuiFlock(mark, unix.LOCK_EX|unix.LOCK_NB); {
	case err == nil:
		// Alone. Turn the mark shared now, behind the gate and before any recovery, so no terminal of
		// this kind can start in the conversion's gap. A conversion that fails would leave this
		// terminal unseen by the next one, so it stops the start like any other hold that cannot be
		// placed; share then only lets the gate go once recovery is done.
		if err := waitLock(mark, unix.LOCK_SH, "another terminal took this workspace's mark"); err != nil {
			return fail(err)
		}
		return release, true, letGateGo, nil
	case classifyLock(err) != lockWait:
		return fail(err)
	}

	// 3. Not alone: take the shared mark (waiting out a terminal of an older build that holds it
	// exclusively while it recovers) and let the gate go.
	if err := waitLock(mark, unix.LOCK_SH, "another terminal here is still recovering"); err != nil {
		return fail(err)
	}
	letGateGo()
	return release, false, noop, nil
}

// waitLock takes how on f, waiting while classifyLock says waiting ends the failure and saying what it
// waits for every tuiShareWait (Ctrl-C stops it). Any other failure is returned as it is.
func waitLock(f *os.File, how int, what string) error {
	deadline := time.Now().Add(tuiShareWait)
	for {
		err := tuiFlock(f, how|unix.LOCK_NB)
		if err == nil || classifyLock(err) != lockWait {
			return err
		}
		if time.Now().After(deadline) {
			if errors.Is(err, unix.ENOLCK) || errors.Is(err, unix.ENOMEM) {
				fmt.Fprintf(tuiStderr, "magi: no file lock can be taken right now (%v); waiting for one — Ctrl-C to stop\n", err)
			} else {
				fmt.Fprintf(tuiStderr, "magi: %s (%v); waiting — Ctrl-C to stop\n", what, err)
			}
			deadline = time.Now().Add(tuiShareWait)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lockFault is what a failed flock means for a terminal's hold.
type lockFault int

const (
	// lockWait: waiting ends it — another terminal holds the lock now, a signal cut the call short, or
	// locks are short for a moment (ENOLCK, ENOMEM) and may come back while terminals run.
	lockWait lockFault = iota
	// lockUnsupported: this filesystem has no flock at all, for every terminal alike.
	lockUnsupported
	// lockBroken: anything else (an I/O error, a bad descriptor). Nothing says it is the same for every
	// terminal or that it lasts, so it is not a reason to run unseen.
	lockBroken
)

// classifyLock is the one place a flock failure is given a meaning. Unknown failures are lockBroken:
// a hold is placed only when it is known to be safe.
func classifyLock(err error) lockFault {
	switch {
	case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EINTR),
		errors.Is(err, unix.ENOLCK), errors.Is(err, unix.ENOMEM):
		return lockWait
	case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP), errors.Is(err, unix.ENOSYS):
		return lockUnsupported
	}
	return lockBroken
}

// tuiShareWait is how often a terminal waiting for a lock says it is still waiting.
var tuiShareWait = 10 * time.Second

// tuiFlock and tuiStderr are flock(2) and where notes go; tests stand in for them.
var (
	tuiFlock            = func(f *os.File, how int) error { return unix.Flock(int(f.Fd()), how) }
	tuiStderr io.Writer = os.Stderr
)
