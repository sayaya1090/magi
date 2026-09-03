//go:build !windows

package daemon

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// claimPath takes exclusive ownership of a socket path for the life of this process, and returns
// the function that gives it back.
//
// Without it, "is anybody listening?" followed by "remove the stale socket and bind" is three steps
// with two gaps, and two daemons starting together fall through both: each dials and finds nothing,
// each removes what the other just bound, and the second one's socket is the only one anyone can
// reach while the first keeps running turns against the same store. Measured at 25 out of 300
// simultaneous starts before this existed — not a corner, a coin flip with a bias.
//
// flock is the right tool because it is atomic and because the kernel drops it when the holder
// dies, however it dies. That is the same problem the stale-socket dance is trying to solve, minus
// the dance: a lock nobody holds is free to take, whether the last owner exited cleanly, crashed,
// or was killed with SIGKILL.
//
// The lock file is never removed. Deleting it would let one process unlink the file another is
// holding a lock on, and both would then hold "the" lock on two different inodes — the same defect
// one layer down. It is an empty file next to the socket.
func claimPath(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: claiming %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon: %s", heldBy(path))
	}
	// Closing the file releases the lock; nothing else needs to happen.
	return func() { f.Close() }, nil
}
