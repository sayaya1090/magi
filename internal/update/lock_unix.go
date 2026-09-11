//go:build !windows

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

// holdInstall takes the install-unit lock for target, or reports that somebody else has it.
//
// Mirrors internal/adapter/daemon's claimPath, for the same reasons: flock is atomic, and the
// kernel drops it when the holder dies however it dies — so a daemon killed mid-update does not
// leave a lock nobody can take. The file is never removed; unlinking it would let one process
// delete the file another holds a lock on, and both would then hold "the" lock on two inodes.
func holdInstall(target string) (func(), bool) {
	f, err := os.OpenFile(target+".update.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, false
	}
	return func() { f.Close() }, true
}
