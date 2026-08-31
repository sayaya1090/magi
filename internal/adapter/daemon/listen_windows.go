package daemon

import (
	"fmt"
	"net"
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
