//go:build !linux && !darwin

package builtin

import "errors"

// portOwnerSupported gates REGISTRATION, not just the call. Advertising a tool that refuses every
// call is how an agent spends steps discovering a door is painted on — the same drift the
// permission allowlist had, and the reason ask_user and council are already withdrawn when they
// cannot work. The platform is known when the binary is built, so there is no run to wait for.
const portOwnerSupported = false

// On the remaining platforms there is neither /proc/net/tcp (Linux) nor a reliable lsof
// (macOS) to ask, so port_owner reports itself unsupported (Execute returns early on the
// false). killOwner exists only to satisfy the build — it is never reached, since Execute
// stops on !supported first.
func findPortOwners(port int) ([]portOwner, bool) { return nil, false }

func killOwner(pid int, sig string) error {
	return errors.New("port_owner is not supported on this platform")
}
