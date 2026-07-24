package builtin

// port_owner finds which process is bound to a TCP port and can kill it, using
// only /proc — no external binary. In stripped containers pkill/pgrep/lsof/ss/
// fuser are absent (exit 127), so an agent that raw-detaches a server (`setsid
// python server.py &`), edits it, and relaunches leaves the OLD copy squatting
// the port: it can't be found to kill, and the grader then hits the stale process
// (observed: kv-store-grpc → StatusCode.UNIMPLEMENTED off a zombie server). `kill`
// is a shell builtin so it always works ONCE you have the pid — the only missing
// piece is FINDING it, which is exactly what this tool restores portably.
//
// Unlike bash_kill (keyed on a magi-tracked bg_N handle), this finds a process
// magi did NOT launch — a raw-detached child or any foreign squatter — by the
// port it holds. The platform split lives in portowner_linux.go / _other.go;
// this file is the platform-independent tool shell.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// tcpStates maps the hex state in /proc/net/tcp to a readable name (the ones worth showing).
var tcpStates = map[string]string{
	"01": "ESTABLISHED", "04": "FIN_WAIT1", "05": "FIN_WAIT2",
	"06": "TIME_WAIT", "08": "CLOSE_WAIT", "09": "LAST_ACK", "0A": "LISTEN",
}

// collectPortInodes parses one /proc/net/tcp{,6} file and records the socket inode
// (and TCP state) of every row whose LOCAL port equals port. Matching the LOCAL
// port selects the server side (listener + its accepted sockets), never a client
// that merely connected TO the port (whose local port is ephemeral). Pure file
// parsing, so it is platform-independent and unit-testable off Linux; only the
// /proc/<pid>/fd cross-reference in findPortOwners is Linux-only.
func collectPortInodes(path string, port int, out map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 {
			continue // header row
		}
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		local := f[1] // "<hexIP>:<hexPORT>"
		colon := strings.LastIndex(local, ":")
		if colon < 0 {
			continue
		}
		p64, err := strconv.ParseInt(local[colon+1:], 16, 32)
		if err != nil || int(p64) != port {
			continue
		}
		inode := f[9]
		if inode == "" || inode == "0" {
			continue // orphaned socket with no owning process (e.g. TIME_WAIT)
		}
		st := tcpStates[f[3]]
		if st == "" {
			st = "state:" + f[3]
		}
		if cur, ok := out[inode]; !ok || (st == "LISTEN" && cur != "LISTEN") {
			out[inode] = st
		}
	}
}

// readCmdline returns a process's command line (NUL-separated in /proc), collapsed
// to spaces and bounded, falling back to /proc/<pid>/comm. Best-effort — an empty
// string just omits the annotation. Off Linux the /proc reads simply fail (empty).
func readCmdline(pid int) string {
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil && len(data) > 0 {
		s := strings.Join(strings.Split(strings.TrimRight(string(data), "\x00"), "\x00"), " ")
		s = strings.TrimSpace(s)
		if len(s) > 200 {
			s = s[:200] + "…"
		}
		if s != "" {
			return s
		}
	}
	if c, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		return strings.TrimSpace(string(c))
	}
	return ""
}

// portOwner is one process holding a TCP port.
type portOwner struct {
	pid   int
	state string // TCP state, e.g. LISTEN, from /proc/net/tcp
	cmd   string // /proc/<pid>/cmdline, best-effort
}

// PortOwner is the port_owner tool: find (and optionally kill) whatever holds a TCP port.
type PortOwner struct{}

func (PortOwner) Name() string { return "port_owner" }
func (PortOwner) Description() string {
	return "Find which process (PID) is bound to a TCP port, reading /proc — works when pkill/pgrep/lsof/ss/fuser are absent (they exit 127 in stripped containers). Returns the owning PID(s) and their command line. Set kill=true to terminate the owner — use this to FREE A PORT that a stale/leftover server still holds before you restart it (a raw-detached `server &` you can no longer reach with bash_kill). Prefer bash_kill{bg_N} for a server YOU started with background=true; use port_owner for a squatter you cannot otherwise find. signal=\"int\"/\"term\" sends a graceful stop instead of a hard kill."
}
func (PortOwner) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"port":{"type":"integer","description":"TCP port to look up (1-65535)"},"kill":{"type":"boolean","description":"terminate the owning process(es) to free the port"},"signal":{"type":"string","enum":["int","term"],"description":"with kill: graceful stop (Ctrl-C equivalent) instead of the default hard kill"}},"required":["port"]}`)
}

func (PortOwner) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		Port   flexInt  `json:"port"`
		Kill   flexBool `json:"kill"`
		Signal string   `json:"signal"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	p := int(a.Port)
	if p < 1 || p > 65535 {
		return errResult("", "port must be an integer in 1..65535"), nil
	}
	owners, supported := findPortOwners(p)
	if !supported {
		return errResult("", "port_owner reads /proc and is only available on Linux (bench containers) — not on this platform"), nil
	}
	if len(owners) == 0 {
		// Not an error: an empty result means the port is FREE — a useful fact
		// (e.g. the stale server already exited, so a restart will bind cleanly).
		return okText("", fmt.Sprintf("no process is bound to TCP port %d — the port is free", p)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "TCP port %d is held by:", p)
	for _, o := range owners {
		fmt.Fprintf(&b, "\n  pid %d", o.pid)
		if o.state != "" {
			fmt.Fprintf(&b, " (%s)", o.state)
		}
		if o.cmd != "" {
			fmt.Fprintf(&b, ": %s", o.cmd)
		}
	}
	if !bool(a.Kill) {
		b.WriteString("\n(pass kill:true to terminate — or `kill <pid>` yourself, the shell builtin always works)")
		return okText("", b.String()), nil
	}

	// Never kill our own process — self-termination is unrecoverable (mirrors the
	// bash self-kill guard). A magi run never legitimately owns a task port, so
	// skipping self can only prevent a fatal mistake.
	self := os.Getpid()
	sig := strings.ToLower(strings.TrimSpace(a.Signal))
	var killed, failed, skipped int
	b.WriteString("\n")
	for _, o := range owners {
		if o.pid == self {
			fmt.Fprintf(&b, "\nskipped pid %d — that is this agent's own process; not killing self", o.pid)
			skipped++
			continue
		}
		if err := killOwner(o.pid, sig); err != nil {
			fmt.Fprintf(&b, "\nfailed to kill pid %d: %v", o.pid, err)
			failed++
			continue
		}
		if sig == "int" || sig == "term" {
			fmt.Fprintf(&b, "\nsent SIG%s to pid %d — re-run port_owner to confirm the port is free", strings.ToUpper(sig), o.pid)
		} else {
			fmt.Fprintf(&b, "\nkilled pid %d", o.pid)
		}
		killed++
	}
	if killed > 0 && failed == 0 && skipped == 0 && sig == "" {
		b.WriteString(fmt.Sprintf("\nport %d should now be free — restart your server", p))
	}
	res := okText("", b.String())
	res.IsError = failed > 0
	return res, nil
}
