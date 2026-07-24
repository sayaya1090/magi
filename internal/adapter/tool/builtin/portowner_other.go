//go:build !linux

package builtin

import "errors"

// On non-Linux platforms there is no /proc/net/tcp to scan, so port_owner reports
// itself unsupported (Execute returns early on the false). killOwner exists only to
// satisfy the build — it is never reached, since Execute stops on !supported first.
func findPortOwners(port int) ([]portOwner, bool) { return nil, false }

func killOwner(pid int, sig string) error {
	return errors.New("port_owner is only supported on Linux")
}
