//go:build !windows

package main

import "syscall"

// detachAttr puts the daemon in a session of its own.
//
// Setsid makes it a session leader with no controlling terminal, so the signals that end a
// terminal's foreground group — the SIGINT of a Ctrl-C, the SIGHUP of a closing shell — do not
// reach it. Its parent exiting leaves it reparented to init, alive.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// detachFallback is the Windows half's escape hatch; there is nothing to fall back from here.
func detachFallback() *syscall.SysProcAttr { return nil }
