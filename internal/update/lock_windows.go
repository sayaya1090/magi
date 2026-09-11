package update

import (
	"os"

	"golang.org/x/sys/windows"
)

// holdInstall takes the install-unit lock for target, or reports that somebody else has it.
//
// LockFileEx is Windows' flock: exclusive, non-blocking with the fail-immediately flag, released by
// the kernel when the handle closes — including when the process dies. Same property the unix build
// relies on, and it matters more here: this is the platform where the file being replaced may also
// be locked by a running image, so an update that dies mid-way must not also leave a stuck lock.
func holdInstall(target string) (func(), bool) {
	f, err := os.OpenFile(target+".update.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	var ov windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ov); err != nil {
		f.Close()
		return nil, false
	}
	return func() { f.Close() }, true
}
