//go:build windows

package graceful

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/quietconsole"
)

// readyEvery is how often the previous generation asks whether its successor is serving yet. A
// quarter second: the question costs a file read and, once the record is there, one bounded dial.
var readyEvery = 250 * time.Millisecond

// exit is os.Exit, named so a test can see this process decide to leave without the test binary
// leaving with it.
var exit = os.Exit

// reexec spawns the binary at path as a successor and, once it is serving, exits this process —
// Windows has no execve, so the image cannot be replaced in place. The daemon has already released
// its socket and lock, so the successor binds fresh; the sub-second window where the socket is down
// is the cost of not having execve, and a client rides it out by retrying. The successor gets its own
// process group so a console Ctrl-C aimed at the (already exiting) parent's group does not take it
// down too; it still shares the console's std handles, which is what a person watching the window
// expects. os.Exit(0) matches the Unix contract that reexec does not return on success.
//
// ⚠ **It waits for ready before leaving, and that is the whole difference from what it was.** It
// used to exit the moment Start() succeeded, so a successor that fell over on its first line was
// seen by nobody: this process was gone, the journal counts only starts that get as far as Resume,
// and a daemon nobody owns has nobody to start it again (CLIENT_LIFECYCLE §4, "a successor that
// dies right after it starts"). This process is the only one still holding the successor's handle,
// so it is the only one that can tell "it died" from "it is taking a while".
//
// Waiting costs nothing the successor needs: its stdin is its own inherited handle, so this process
// closing its copy of the owner's pipe on the way out cannot look like the owner closing — EOF
// needs every WRITE end gone, and the only write end is the owner's.
func reexec(path string, argv []string, env []string, ready Ready) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = reexecAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	if ready != nil {
		if err := awaitSuccessor(cmd, ready); err != nil {
			return err
		}
	}
	exit(0)
	return nil // unreachable outside a test
}

// awaitSuccessor watches the successor until it is serving, ends, or runs out of time — whichever
// comes first, and they are three different answers.
func awaitSuccessor(cmd *exec.Cmd, ready Ready) error {
	pid := cmd.Process.Pid
	ended := make(chan *SuccessorDied, 1)
	go func() {
		err := cmd.Wait()
		died := &SuccessorDied{PID: pid, Code: -1}
		if cmd.ProcessState != nil {
			// A non-zero exit also comes back as an *exec.ExitError, and the code already says it.
			died.Code = cmd.ProcessState.ExitCode()
		} else {
			// No state at all: the wait itself failed, and that is the only account there is.
			died.Err = err
		}
		ended <- died
	}()
	deadline := time.NewTimer(ReadyWithin)
	defer deadline.Stop()
	tick := time.NewTicker(readyEvery)
	defer tick.Stop()
	for {
		select {
		case died := <-ended:
			return died
		case <-deadline.C:
			return &NotReady{PID: pid, Within: ReadyWithin}
		case <-tick.C:
			if ready(pid) {
				return nil
			}
		}
	}
}

// PipeClosed reports whether every write end of the pipe f reads from is gone — for the owned
// daemon's stdin, whether its owner has let go — WITHOUT reading from it.
//
// Peeking rather than reading is the point: this is asked by a process that is about to hand the
// same pipe to another successor, and a read that consumed anything would take it from the process
// the pipe is meant for. known is false for a handle that is not a pipe at all (a console, NUL, a
// file), where the question has no answer and the caller must not act as if it had one.
func PipeClosed(f *os.File) (closed, known bool) {
	if f == nil {
		return false, false
	}
	var avail uint32
	ok, _, err := procPeekNamedPipe.Call(f.Fd(), 0, 0, 0, uintptr(unsafe.Pointer(&avail)), 0)
	switch {
	case ok != 0:
		return false, true
	case errors.Is(err, windows.ERROR_BROKEN_PIPE):
		return true, true
	default:
		return false, false
	}
}

// Not in golang.org/x/sys/windows at the version this module pins, so called the way quietconsole
// calls GetConsoleWindow.
var procPeekNamedPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

// reexecAttr keeps the successor in the console situation the parent is in.
//
// In a terminal the parent has a console and the successor inherits it — a person watching the window
// keeps watching. A daemon started detached has NO console, and a console program started by such a
// parent is given a brand-new VISIBLE console by Windows, titled with the exe path. That was the black
// "magi.exe" window that stayed on the taskbar after every "restarting onto the binary on disk"
// (2026-09-06, LTSC 2021 machine: conhost titled C:\...\magi\ppt\magi.exe, born the moment the daemon
// restarted). So without a console the successor is started DETACHED_PROCESS, exactly as
// cmd/magi/detach.go starts the first daemon — no console, stdout/stderr still the inherited log file.
//
// The flags below are compared by a test next door; whether Windows then gave the successor a console
// anyway is a different question, and `TestTheSuccessorOfADetachedDaemonHasNoConsoleEither`
// (cmd/magi) is the one that asks it — it restarts a real detached daemon and asks the operating
// system, through AttachConsole, what the successor ended up with.
func reexecAttr() *syscall.SysProcAttr {
	flags := uint32(syscall.CREATE_NEW_PROCESS_GROUP)
	if !quietconsole.HasConsole() {
		flags |= windows.DETACHED_PROCESS
	}
	return &syscall.SysProcAttr{CreationFlags: flags}
}
