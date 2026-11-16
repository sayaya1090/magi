package builtin

import (
	"fmt"
	"os"
	"syscall"
)

// signalOf names the signal that killed the process itself, or "" when it exited on its own. Go
// reports such a process as ExitCode -1, which on its own reads as a program that chose to return
// -1; the wait status still knows the signal. (On Windows Signaled is always false.)
func signalOf(ps *os.ProcessState) string {
	if ps == nil {
		return ""
	}
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return ""
	}
	return fmt.Sprintf("killed by signal %d (%s)", int(ws.Signal()), ws.Signal())
}

// outsideKillLine is the line under "exit N" for a command killed by something other than magi —
// on every result, not only an empty one, because a partial output reads like a complete one.
func outsideKillLine(killed string) string {
	if killed == "" {
		return ""
	}
	return "[" + killed + " — not by magi; the command did not finish, so its output stops where it was killed]\n"
}
