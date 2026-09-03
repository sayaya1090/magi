//go:build windows

package daemon

import "os"

// processAlive on Windows: FindProcess actually looks, unlike the unix build where it always
// succeeds. A process that cannot be opened is reported as "not known" rather than dead — the
// handle may be refused for reasons that have nothing to do with whether it is running.
func processAlive(pid int) (alive, known bool) {
	if pid <= 0 {
		return false, false
	}
	if _, err := os.FindProcess(pid); err != nil {
		return false, false
	}
	return true, true
}
