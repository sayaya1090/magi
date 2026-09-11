package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/graceful"
	"github.com/sayaya1090/magi/internal/update"
	"github.com/sayaya1090/magi/internal/version"
)

// restarting is where the daemon that asked to be relaunched was serving, set by run() once it knows
// and read by main() after run() has let all of it go. The successor is judged against these: the
// same socket, the same workspace, and — when a window owns this daemon — the same lineage.
var restarting struct {
	socket, workdir string
	owned           bool
}

// successorReady is CLIENT_LIFECYCLE §4's "ready" (discovery and launch, step 4) for the process this
// one just started: the record on the workspace's socket was written by THAT pid, for this workspace
// and this lineage, and a handshake on the socket reaches the process that wrote it. A file
// appearing is not enough (the same step: "not process creation or file creation alone"), and
// neither is a socket answering — during the gap another magi could have taken the workspace, and
// its answers would look exactly as good.
func successorReady(socket, workdir, owner string) graceful.Ready {
	return func(pid int) bool {
		rec, ok := serving(socket)
		if !ok || rec.PID != pid || !daemon.SamePath(rec.Workdir, workdir) || rec.Owner != owner {
			return false
		}
		return true
	}
}

// serving reads the record on socket and checks it against a handshake: the daemon that answers is
// the one the record says. An instance missing on either side is an older build, which is "cannot
// tell" and not "a different process" (daemon.PeerInfo.Instance), so only two present values that
// differ fail it.
func serving(socket string) (daemon.Info, bool) {
	rec, err := daemon.Published(socket)
	if err != nil {
		return daemon.Info{}, false
	}
	c, err := daemon.DialWithin(socket, time.Second, 2*time.Second)
	if err != nil {
		return daemon.Info{}, false
	}
	defer c.Close()
	peer, err := c.Hello()
	if err != nil {
		return daemon.Info{}, false
	}
	if rec.Instance != "" && peer.Instance != "" && rec.Instance != peer.Instance {
		return daemon.Info{}, false
	}
	return rec, true
}

// afterRelaunch is what the previous generation does with what its successor did. Separate from the
// calls it makes so the decisions can be measured without a daemon — every branch here is a rule
// from CLIENT_LIFECYCLE, and a rule nobody measures is a rule nobody notices breaking.
type afterRelaunch struct {
	// relaunch starts a successor onto the binary on disk and waits for it (graceful.Reexec).
	relaunch func() error
	// someoneElse says another daemon is serving this workspace now, and which pid.
	someoneElse func() (int, bool)
	// failed tells the update journal this process watched its successor die (update.SuccessorFailed).
	failed func(successor int) (update.Recovery, error)
	// ownerGone says the window that owns this daemon has let go of the pipe. Always false for a
	// daemon nobody owns.
	ownerGone func() bool
	say       io.Writer
}

// run takes the error from the FIRST relaunch — the one run() asked for — and returns the exit code.
func (a afterRelaunch) run(err error, code int) int {
	var slow *graceful.NotReady
	var died *graceful.SuccessorDied
	switch {
	case err == nil:
		return code
	case errors.As(err, &slow):
		// Left running on purpose — see graceful.NotReady. The journal's window judges it from here.
		fmt.Fprintln(a.say, "magi: restart:", slow)
		return 0
	case !errors.As(err, &died):
		// It never started. The update simply did not take effect this time.
		fmt.Fprintln(a.say, "magi: restart:", err)
		return code
	}
	fmt.Fprintln(a.say, "magi: restart:", died)
	// Stopped on purpose — its owner closed the pipe, somebody asked it to shut down. Ending before
	// serving is what a deliberate stop looks like from here too, and it is not a build falling over.
	if died.Code == 0 {
		return 0
	}
	// Somebody else took the workspace in the gap between this generation letting go and the
	// successor binding. The successor lost that race; the candidate did not fail. Rolling it back
	// would be undoing an update because of a timing accident.
	if pid, ok := a.someoneElse(); ok {
		fmt.Fprintf(a.say, "magi: restart: another daemon (pid %d) has this workspace now — "+
			"not reading that as the new build failing\n", pid)
		return 0
	}
	rec, ferr := a.failed(died.PID)
	switch {
	case ferr != nil:
		fmt.Fprintln(a.say, "magi: restart: the update journal could not be settled:", ferr)
		return 1
	case !rec.RolledBack:
		// A restart onto the same build, or a candidate another workspace is running fine. There is
		// no previous build to go back to and nothing to refuse — so say it, and end with an error so
		// whatever started this daemon counts it as the failure it is.
		fmt.Fprintln(a.say, "magi: restart: no previous build to go back to — the companion is not "+
			"running; start it again once the cause in the lines above is fixed")
		return 1
	}
	fmt.Fprintf(a.say, "magi: restart: %s did not come up — %s is back on disk, and %s will not be "+
		"taken again on its own (`magi -update` retries it)\n", rec.To, rec.From, rec.To)
	// CLIENT_LIFECYCLE §9.3, step 6: relaunch the previous build only while its owner is still there.
	// A window that has closed gets its file back and no process — a companion coming back for a window that is gone
	// is exactly the survivor the owner's pipe exists to prevent.
	if a.ownerGone() {
		fmt.Fprintln(a.say, "magi: restart: its window has closed, so the previous build is not relaunched")
		return 1
	}
	// Once. The build going back up is the one that was serving a moment ago, and if it will not come
	// up either there is nothing left on disk to try.
	err = a.relaunch()
	switch {
	case err == nil:
		return 0
	case errors.As(err, &slow):
		fmt.Fprintln(a.say, "magi: restart:", slow)
		return 0
	}
	fmt.Fprintf(a.say, "magi: restart: the previous build %s did not come up either (%v) — the companion "+
		"is not running; start it by hand with `magi --daemon` in its workspace\n", rec.From, err)
	return 1
}

// relaunchOnExit is main()'s restart step: relaunch, wait for the successor, and decide.
//
//coverage:ignore wiring — every decision it reaches is afterRelaunch's, and measured there
func relaunchOnExit(code int) int {
	var ready graceful.Ready
	if restarting.socket != "" {
		ready = successorReady(restarting.socket, restarting.workdir, daemon.OwnerID())
	}
	relaunch := func() error { return graceful.Reexec(ready) }
	return afterRelaunch{
		relaunch: relaunch,
		someoneElse: func() (int, bool) {
			if restarting.socket == "" {
				return 0, false
			}
			rec, ok := serving(restarting.socket)
			return rec.PID, ok
		},
		failed: func(successor int) (update.Recovery, error) {
			exe, err := os.Executable()
			if err != nil {
				return update.Recovery{}, err
			}
			return update.SuccessorFailed(exe, version.Version, successor)
		},
		ownerGone: func() bool {
			if !restarting.owned {
				return false
			}
			closed, known := graceful.PipeClosed(os.Stdin)
			return closed && known
		},
		say: os.Stderr,
	}.run(relaunch(), code)
}
