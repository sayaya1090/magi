package daemon

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"
)

// listenOwnerOnly opens the control socket. Windows has no umask: an AF_UNIX socket there inherits
// the containing directory's ACL, which is what the config directory's own permissions decide.
func listenOwnerOnly(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("daemon: listen: %w", err)
	}
	return ln, nil
}

// secureSocket does nothing on Windows, and the nothing is the point.
//
// There is no mode to confirm: an AF_UNIX socket here inherits the containing directory's ACL, and
// the config directory is the account's own. Asking anyway is not free — a chmod on the socket
// answers "The file cannot be accessed by the system" and, when that error was fatal, `magi
// --daemon` could not start on Windows at all. The failure named a permission bit; the cause was
// the platform.
//
// So the confinement Windows actually gets is the directory's, and this says that rather than
// pretending a mode was set.
func secureSocket(string) error { return nil }

// removeSocket deletes a socket file that nothing is listening on any more.
//
// os.Remove cannot. Windows implements an AF_UNIX socket as a REPARSE POINT, and DeleteFileW on one
// answers ERROR_CANT_ACCESS_FILE — "The file cannot be accessed by the system" — unless it is opened
// without following the reparse. So a daemon that was killed left a file behind that nothing could
// remove: not os.Remove, not `del`, not `fsutil`. The workspace was then unusable for good, because
// Listen's own recovery path ends in exactly that failed remove, and a dial to the dangling file
// answers "An invalid argument was supplied" rather than "nobody is listening".
//
// Opening it with FILE_FLAG_OPEN_REPARSE_POINT and FILE_FLAG_DELETE_ON_CLOSE deletes it on the
// handle's close — one open, no second syscall to get wrong.
func removeSocket(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_DELETE_ON_CLOSE|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return os.ErrNotExist
		}
		// Fall back to the ordinary delete: the path may not be a socket at all (Listen checks
		// that separately), and an ordinary file is removed the ordinary way.
		if rerr := os.Remove(path); rerr == nil {
			return nil
		}
		return err
	}
	return windows.CloseHandle(h)
}
