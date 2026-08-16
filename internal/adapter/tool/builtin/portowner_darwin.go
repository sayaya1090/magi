//go:build darwin

package builtin

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// macOS has no /proc, but it always ships lsof — so the same question is answered by asking it.
// Observed live (the server-lifecycle wave run, 2026-08-16): the flat "only available on Linux"
// refusal pushed the model into pkill guesswork on the one platform this machine actually runs;
// the tool's whole point is that finding the pid is the missing piece.
//
// Only the LOCAL port is matched, mirroring the Linux scan: the server side (listener + accepted
// sockets), never a client that merely connected TO the port.
func findPortOwners(p int) ([]portOwner, bool) {
	if _, err := exec.LookPath("lsof"); err != nil {
		return nil, false // a Mac without lsof is a stripped environment this cannot serve
	}
	// -F machine format: p<pid> / c<command> once per process, then n<name> (+ TST= state) per
	// socket. lsof exits nonzero when nothing matches, which here means the port is FREE.
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(p), "-FpcnT").Output()
	if err != nil {
		return nil, true
	}
	var owners []portOwner
	byPID := map[int]int{} // pid -> index in owners
	cur, curCmd := 0, ""
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			cur, _ = strconv.Atoi(val)
			curCmd = ""
		case 'c':
			curCmd = val
		case 'T':
			if st, ok := strings.CutPrefix(val, "ST="); ok && cur != 0 {
				if i, seen := byPID[cur]; seen && owners[i].state != "LISTEN" {
					owners[i].state = st
				}
			}
		case 'n':
			// "local" or "local->remote"; the LOCAL half decides ownership.
			local := val
			if i := strings.Index(local, "->"); i >= 0 {
				local = local[:i]
			}
			colon := strings.LastIndex(local, ":")
			if colon < 0 || cur == 0 {
				continue
			}
			if lp, err := strconv.Atoi(local[colon+1:]); err != nil || lp != p {
				continue
			}
			if _, seen := byPID[cur]; !seen {
				byPID[cur] = len(owners)
				owners = append(owners, portOwner{pid: cur, cmd: curCmd})
			}
		}
	}
	return owners, true
}

// killOwner mirrors the Linux one: a single pid, not its group — the LISTEN socket that blocks a
// rebind is held by that exact process, so a precise kill frees the port without collateral.
func killOwner(pid int, sig string) error {
	s := syscall.SIGKILL
	switch sig {
	case "int":
		s = syscall.SIGINT
	case "term":
		s = syscall.SIGTERM
	}
	return syscall.Kill(pid, s)
}
