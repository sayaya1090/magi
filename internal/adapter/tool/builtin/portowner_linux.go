//go:build linux

package builtin

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// findPortOwners returns the processes whose LOCAL TCP port equals port, by
// cross-referencing socket inodes from /proc/net/tcp{,6} against the /proc/<pid>/fd
// symlinks (which resolve to "socket:[<inode>]"). The second return is whether the
// platform is supported (always true here — the !linux build returns false).
func findPortOwners(port int) ([]portOwner, bool) {
	inodeState := map[string]string{} // socket inode -> TCP state name
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		collectPortInodes(path, port, inodeState)
	}
	if len(inodeState) == 0 {
		return nil, true // supported, but nothing holds the port
	}
	pidState := map[int]string{} // pid -> best (LISTEN-preferred) state
	procs, _ := os.ReadDir("/proc")
	for _, e := range procs {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // non-numeric /proc entry
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // process gone or not readable by us
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			ino, ok := strings.CutPrefix(link, "socket:[")
			if !ok {
				continue
			}
			ino = strings.TrimSuffix(ino, "]")
			st, held := inodeState[ino]
			if !held {
				continue
			}
			if cur, seen := pidState[pid]; !seen || (st == "LISTEN" && cur != "LISTEN") {
				pidState[pid] = st
			}
		}
	}
	out := make([]portOwner, 0, len(pidState))
	for pid, st := range pidState {
		out = append(out, portOwner{pid: pid, state: st, cmd: readCmdline(pid)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out, true
}

// killOwner sends a signal to a single pid (not its group): the LISTEN socket that
// blocks a rebind is held by that exact process, so a precise kill frees the port
// without collateral. Default is a hard SIGKILL; "int"/"term" are graceful.
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
