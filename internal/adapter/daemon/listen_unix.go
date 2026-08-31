//go:build !windows

package daemon

import (
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
)

// umaskMu serialises the umask flip. The umask is per-PROCESS, so two daemons starting in one
// process (tests do this) could otherwise restore each other's value and leave it flipped for
// whatever else the process creates.
var umaskMu sync.Mutex

// listenOwnerOnly opens the control socket with owner-only permissions from the first instant it
// exists.
//
// Chmod after Listen is not enough. A unix socket is created with the process umask — rwxr-xr-x
// under the usual 022 — and it starts accepting the moment it exists, because the kernel queues
// connections before anything calls Accept. So between those two lines there is a socket any local
// user can connect to, on a channel whose whole purpose is making this engine run commands with
// this user's permissions. Measured: the mode really is rwxr-xr-x in that window.
//
// The umask is the only hook the OS offers here — there is no atomic "create with mode" for a unix
// socket in Go — so it is flipped for exactly the length of the Listen call and put back.
func listenOwnerOnly(path string) (net.Listener, error) {
	umaskMu.Lock()
	old := syscall.Umask(0o177) // clear group and other on anything created below
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	umaskMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("daemon: listen: %w", err)
	}
	return ln, nil
}

// secureSocket confirms the mode the umask flip above already set.
//
// Belt and braces: listenOwnerOnly creates it at 0600, and this costs one syscall to be sure. It is
// separate from that function because the platform without a mode has nothing to confirm — see the
// Windows half.
func secureSocket(path string) error { return os.Chmod(path, 0o600) }

// removeSocket deletes a socket file nothing is listening on. Ordinary on unix; the Windows half
// has to open the reparse point to do the same thing.
func removeSocket(path string) error { return os.Remove(path) }
