package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachAttr asks Windows for a process with no console and no inherited job.
//
// DETACHED_PROCESS keeps it off the caller's console, so a console that closes does not take it
// down. CREATE_NEW_PROCESS_GROUP keeps a Ctrl-C sent to the caller's group away from it.
// CREATE_BREAKAWAY_FROM_JOB is the one that matters for an IDE: a child started by a program that
// runs inside a job object is in that job, and when the job is closed every process in it is
// killed — which is precisely how an editor takes its daemon with it.
//
// What the flags ASK for is checked in detach_attr_windows_test.go. What Windows then did with them
// is a separate question and has its own tests: detach_live_windows_test.go starts a real detached
// daemon inside a real job object, closes the job, and asks the operating system whether the daemon
// is still there and whether it ended up holding a console.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS |
		windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_BREAKAWAY_FROM_JOB}
}

// detachFallback drops the breakaway.
//
// A job may forbid breaking out of it (JOB_OBJECT_LIMIT_BREAKAWAY_OK unset), and asking anyway
// fails the whole spawn. Then the daemon cannot outlive that job — but it can still survive the
// console closing and the Ctrl-C, so it is started that way rather than not at all.
func detachFallback() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}
