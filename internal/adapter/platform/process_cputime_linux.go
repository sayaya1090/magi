//go:build linux

package platform

import (
	"os"
	"strconv"
	"time"
)

// clockTick is the kernel's USER_HZ: utime/stime in /proc/<pid>/stat are counted
// in these ticks. It is 100 on effectively every Linux the worker runs on; reading
// it via cgo (sysconf(_SC_CLK_TCK)) would break the pure-Go cross-compile invariant
// (§9.5), so we assume the standard 100. A wrong constant only rescales the delta,
// which the lease gate compares against a tolerant threshold, so it is not sensitive.
const clockTick = 100

// ProcessCPUTime reads utime+stime from /proc/<pid>/stat. The comm field (2) can
// itself contain spaces and parentheses, so we scan from the LAST ')' — everything
// after it is space-separated and fixed-position: state is field 3, so utime/stime
// (fields 14/15 overall) are indices 11/12 in the post-')' split.
func (OS) ProcessCPUTime(pid int) (time.Duration, bool) {
	if pid <= 0 {
		return 0, false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false // no such process (or unreadable) => treat as dead
	}
	s := string(b)
	rparen := lastIndexByte(s, ')')
	if rparen < 0 || rparen+2 >= len(s) {
		return 0, true // alive but unparseable => liveness only
	}
	fields := splitFields(s[rparen+2:])
	// After the ')': [0]=state ... utime is overall field 14 => index 11, stime => 12.
	if len(fields) < 13 {
		return 0, true
	}
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, true
	}
	ticks := utime + stime
	return time.Duration(ticks) * time.Second / clockTick, true
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// splitFields splits on single spaces (the /proc stat separator) without pulling in
// strings.Fields' unicode handling — the fields are ASCII numbers.
func splitFields(s string) []string {
	var out []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\n' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
