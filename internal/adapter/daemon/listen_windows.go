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
