package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// Starting the daemon so it OUTLIVES whoever started it.
//
// `magi --daemon` serves in the foreground: it is a service you run, and Ctrl-C stops it. That is
// right for a terminal and wrong for every program that wants a companion running in a workspace,
// because a child started with the ordinary means stays in its parent's process group on unix and
// inside its job on Windows, and dies with it. The IDE plugin measured exactly that — a daemon it
// started went away when the IDE did, and its manual had promised otherwise.
//
// The caller cannot fix this from outside. The two ways to detach are a new session (setsid) and
// Windows creation flags, and the JVM can reach neither: ProcessBuilder has no API for creation
// flags, and macOS has no setsid(1) to fall back on. Only the process being started can put itself
// somewhere its parent's death does not reach — so it does, when asked.
//
// Asked, not always: this does not change what `magi --daemon` has always done. It is a second
// thing a caller may want, and it says so.

// detachLogSuffix names where a detached daemon's own words go.
//
// It must not inherit the caller's pipes. A detached process whose stdout is a pipe nobody reads
// blocks on a full buffer, and one whose reader has gone gets EPIPE — either way the thing that was
// supposed to survive its parent is killed by its parent's absence, which is the bug this exists to
// avoid. A file beside the socket also means the reason a start failed is READABLE afterwards.
const detachLogSuffix = ".log"

// argvWithoutDetach is the successor's arguments: this process's own, minus the flag that asked for
// the detachment. Without the removal the child asks to detach too, and detaches, and so on.
//
// Every spelling the flag package accepts is dropped: -detach, --detach, and the =value forms. A
// separated value (`-detach true`) is not a thing for a bool flag — the flag package reads bools
// only as -flag or -flag=value — so there is no second token to remove.
func argvWithoutDetach(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		name := strings.TrimLeft(a, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if strings.HasPrefix(a, "-") && name == "detach" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// detachWait bounds how long the starter waits to be told the daemon is listening. Generous: a cold
// start reads config, opens the store and may probe a context window before it binds.
var detachWait = 30 * time.Second

// startDetached spawns this binary again, in its own session, and waits until its socket answers.
//
// The wait is the difference between "started something" and "it is up". A caller that gets a
// zero exit knows there is a companion listening at that path; without it the honest report would
// be a launch, and the next thing to happen would be a connection refused with nobody to blame.
//
// dial is the liveness question (daemon.Dial in production, a stub in tests).
func startDetached(sockPath string, argv []string, dial func(string) error, errOut io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(errOut, "magi: detach: this binary's own path is unknown:", err)
		return 1
	}
	logPath := sockPath + detachLogSuffix
	logFile, lerr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if lerr != nil {
		fmt.Fprintln(errOut, "magi: detach:", lerr)
		return 1
	}
	defer logFile.Close()

	cmd := exec.Command(exe, argvWithoutDetach(argv)...)
	cmd.Env = os.Environ()
	// No inherited pipes — see detachLogSuffix. Stdin is nothing at all: a daemon reads no input,
	// and leaving the caller's terminal attached is how a background process ends up stopped by
	// SIGTTIN.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, logFile, logFile
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		if retry := detachFallback(); retry != nil {
			// Windows refuses CREATE_BREAKAWAY_FROM_JOB when the job it is in does not permit it.
			// Half a detachment is better than none: a new process group still survives a Ctrl-C
			// sent to the caller's group.
			cmd = exec.Command(exe, argvWithoutDetach(argv)...)
			cmd.Env = os.Environ()
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, logFile, logFile
			cmd.SysProcAttr = retry
			err = cmd.Start()
		}
		if err != nil {
			fmt.Fprintln(errOut, "magi: detach:", err)
			return 1
		}
	}

	// Its death is a fact this waiter needs; without it a daemon that refused to start (the
	// workspace is already claimed, the socket path is too long) is waited on for the full bound
	// and then reported as a timeout — the wrong sentence, and the right one is in the log.
	gone := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(gone) }()

	deadline := time.Now().Add(detachWait)
	for {
		if dial(sockPath) == nil {
			fmt.Fprintf(errOut, "magi: daemon on %s — it outlives this command (log: %s)\n", sockPath, logPath)
			return 0
		}
		select {
		case <-gone:
			fmt.Fprintf(errOut, "magi: detach: the daemon exited before it was listening. %s\n", lastLines(logPath, 5))
			return 1
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(errOut, "magi: detach: nothing was listening on %s after %s; it may still be starting (log: %s)\n",
				sockPath, detachWait, logPath)
			return 1
		}
	}
}

// lastLines is the tail of a file, for a failure whose reason is in it. A report that names a log
// nobody will open is a report that says nothing.
func lastLines(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "Nothing was written to " + path + "."
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return "It wrote nothing to " + path + "."
	}
	return "It said: " + strings.Join(lines, " / ")
}

// dialSocket is the production liveness question: somebody is listening at this path.
func dialSocket(path string) error {
	c, err := daemon.Dial(path)
	if err != nil {
		return err
	}
	return c.Close()
}
