//go:build !linux && !darwin

package builtin

import "errors"

// On the remaining platforms there is neither /proc/net/tcp (Linux) nor a reliable lsof
// (macOS) to ask, so port_owner reports itself unsupported (Execute returns early on the
// false). killOwner exists only to satisfy the build — it is never reached, since Execute
// stops on !supported first.
func findPortOwners(port int) ([]portOwner, bool) { return nil, false }

func killOwner(pid int, sig string) error {
	return errors.New("port_owner is not supported on this platform")
}
