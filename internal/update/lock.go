package update

import (
	"fmt"
	"time"
)

// installWait is how long a settle step waits for a live replacement to finish before giving up.
//
// A transaction's other steps — Salvage, Resume, Confirm, Retry, LeftCleanly — are fast: a small
// JSON read, sometimes a file copy. What they can collide with is a Commit, whose length is
// dominated by the pre-flight (`verifyTimeout`, fifteen seconds) plus the file work around it. So
// this is that, doubled: long enough that a settle step outlasts one honest replacement, short
// enough that a daemon starting next to a wedged one still starts.
//
// ⚠ **This is NOT the rule Commit follows, and the difference is deliberate.** CLIENT_LIFECYCLE §9.3
// says an updater that cannot take the lock must not block, retry in a loop, or steal — it says so
// and carries on, because a daemon queueing to perform an update it does not need is a daemon not
// doing its job. A settle step has no such choice: it is finishing a transaction that already
// exists, and the alternative to waiting is acting on files another process is in the middle of
// rewriting. Measured, two processes: a Salvage that did not wait restored the previous build over a
// live Commit and deleted the backup that Commit's own rollback depends on.
var installWait = 30 * time.Second

// installPoll is how often the wait re-asks. Short enough not to add a visible delay to the common
// case (the lock is free on the first try), long enough not to spin.
const installPoll = 25 * time.Millisecond

// takeInstall holds the install-unit lock for target, waiting out a replacement in progress.
//
// Returns false only when the wait ran out — which means a process has held this install's lock for
// half a minute, and acting anyway is exactly what the lock is there to stop. Nothing is left behind
// by a holder that dies: both platforms use a lock the kernel releases when the handle closes.
func takeInstall(target string) (func(), bool) {
	deadline := time.Now().Add(installWait)
	for {
		if release, ok := holdInstall(target); ok {
			return release, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(installPoll)
	}
}

// errInstallHeld is what a settle step reports when it never got the lock. Named rather than
// invented at each site so the five read identically — and so it says what to look for.
func errInstallHeld(target string) error {
	return fmt.Errorf("another process has held %s.update.lock for %s — its update did not finish, "+
		"and settling this install's journal while it holds the lock would fight it", target, installWait)
}
