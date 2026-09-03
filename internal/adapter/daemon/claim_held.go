package daemon

import (
	"fmt"
	"strings"
	"time"
)

// heldBy says who is holding a socket path, for the error a failed claim returns.
//
// "another magi is starting or running on <path>" was the whole message, and the obvious next
// question — WHICH one — had no answer in it. Measured twice in one session: the reader went to
// `ps` and `lsof`, could not match a process to the path, concluded the lock was a leftover, and
// deleted the lock file. That is the one move the claim exists to prevent. Deleting it does not
// take the lock off anybody: flock lives on the inode, so the next daemon locks a NEW file and two
// processes then hold "the" lock on one socket path, which is the defect one layer down.
//
// The lock itself is right and is not what changed. flock is released by the kernel however the
// holder dies, so a lock that is held is held by something alive. What was missing is a sentence
// that lets a person confirm that for themselves.
//
// Both platforms call this, so the message cannot drift between them — a rule written twice is a
// rule that goes wrong in the copy nobody is looking at.
func heldBy(path string) string {
	in, err := Published(path)
	if err != nil {
		// No record beside the socket. A daemon writes one right after it claims, so the usual
		// reason is that the holder is still starting — a second or two old.
		return "another magi holds " + path + " (it publishes no record yet, so it is probably " +
			"still starting — try again in a moment)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "another magi holds %s", path)
	if in.PID > 0 {
		fmt.Fprintf(&b, " — pid %d", in.PID)
		if alive, known := processAlive(in.PID); known && !alive {
			// The lock is held and the recorded pid is gone. Both are true and they are about
			// different processes: the record is from an earlier daemon, and the holder is
			// somebody newer who has not published yet. Say so rather than inviting the delete.
			b.WriteString(" (which is no longer running — so the holder is a newer one that has " +
				"not published yet; do not remove the .lock file, it will not free anything)")
		}
	}
	if in.Workdir != "" {
		fmt.Fprintf(&b, ", workspace %s", in.Workdir)
	}
	if t, terr := time.Parse(time.RFC3339, in.Started); terr == nil {
		fmt.Fprintf(&b, ", up since %s", t.Local().Format("15:04"))
	}
	b.WriteString(". Stop it with `magi --stop` in that workspace")
	return b.String()
}
