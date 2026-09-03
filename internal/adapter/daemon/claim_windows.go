package daemon

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// claimPath takes exclusive ownership of a socket path for the life of this process.
//
// LockFileEx is Windows' flock: exclusive, non-blocking with the fail-immediately flag, and
// released by the kernel when the holder's handle closes — including when the process dies. Same
// property the unix build relies on, so a crash does not leave a claim nobody can take.
func claimPath(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: claiming %s: %w", path, err)
	}
	var ov windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ov)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon: %s", heldBy(path))
	}
	return func() { f.Close() }, nil
}
