package builtin

import (
	"os"
	"strconv"
	"strings"
)

// procStart reads when the process pid started: field 22 of /proc/<pid>/stat, in clock ticks
// since boot. The command name (field 2) may hold spaces, so fields are counted after its ')'.
func procStart(pid int) (string, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return "", false
	}
	return f[19], true
}
