// A daemon says where it is, in a file that lives and dies with its socket — and finding one.
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	osuser "os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/version"
)

// SocketPath is where a workspace's daemon listens.
//
// Derived from the workspace so two projects do not fight over one path, and placed under the
// config directory rather than the workspace so it never lands in a deliverable tree or a git
// status. The name carries the base directory so `ls` is readable by a person looking for theirs.
func SocketPath(configDir, workdir string) string {
	return filepath.Join(configDir, "daemon-"+WorkspaceKey(workdir)+".sock")
}

// WorkspaceKey names a workspace in one short, stable string: its base directory, so `ls` is
// readable by a person looking for theirs, and a hash of the whole path, so two checkouts of one
// repo are two companions.
//
// One definition because there are two users now — the socket above, and the per-companion config
// directory beside it — and a key spelled two ways would give one companion two identities: it
// would answer on one socket and read settings written for another.
func WorkspaceKey(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	// Resolve symlinks, or one directory gets two names and the daemon and the attach look at
	// different sockets. Go's os.Getwd prefers $PWD when it points at the same place, so a shell
	// that did `cd /tmp/x` reports the logical path while a process that chdir'd itself reports
	// /private/tmp/x — same directory, different hash, "no daemon here". Observed exactly that way.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return sanitize(filepath.Base(abs)) + "-" + shortHash(abs)
}

// maxSocketPath is what the OS allows in a unix address: 104 bytes on macOS, 108 on Linux. Past it
// both bind and connect fail with "invalid argument", which says nothing about the length — so the
// check is here, where the reason can be given.
const maxSocketPath = 100

// tooLong reports a path the OS will refuse, with the reason it will not give.
func tooLong(path string) error {
	if len(path) <= maxSocketPath {
		return nil
	}
	return fmt.Errorf("daemon: the socket path is %d bytes and the OS allows about %d — "+
		"set MAGI_CONFIG_DIR to somewhere shorter: %s", len(path), maxSocketPath, path)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

// shortHash keeps the socket name unique per absolute path while staying inside the ~104-byte limit
// a unix socket path has on macOS — a full path in the name would blow past it.
func shortHash(s string) string {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var out [8]byte
	for i := range out {
		out[i] = digits[h%36]
		h /= 36
	}
	return string(out[:])
}

// Info is what a daemon publishes about itself.
type Info struct {
	Socket  string `json:"socket"`
	Workdir string `json:"workdir"` // the FULL path; the socket name carries only a base name and a hash
	Session string `json:"session"`
	// Name and Role are what a TEAM is addressed by, declared in the workspace's own
	// .magi/config.toml and published here so everything that lists companions reads one source.
	//
	// Without them a companion is identified by the base name of a directory, which answers "which
	// one is this" and not "which one do I want" — and "which one do I want" is the question
	// somebody coordinating work actually has. A directory called `ds` is not a design specialist
	// until it says so.
	//
	// Both optional. A companion with neither is exactly what companions were before: a workspace.
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
	// Team is the group of companions doing related work, and Hub marks the one that answers for
	// it. Addressing, not topology: nothing routes through a hub and nothing is hidden behind one.
	Team string `json:"team,omitempty"`
	Hub  bool   `json:"hub,omitempty"`
	// Can is how many things this companion advertises being able to do, counted by the process
	// that knows: its own skills and its own tool servers. Published rather than worked out by
	// whoever is reading, because a reader on another machine cannot see either.
	//
	// Counted once, when the daemon starts. A skill written this afternoon does not raise it until
	// the companion is next restarted, and that is the right trade: the number decides a tie in a
	// hub election, and an election every companion has to agree on is better served by a value
	// that changes on the scale of days than by one that changes while they are comparing it.
	Can int `json:"can,omitempty"`
	// Does names them, capped. See cluster.Member.Does: names travel, descriptions are fetched.
	Does []string `json:"does,omitempty"`
	// Waiting is how many pieces of handed-over work this companion has taken and not started.
	//
	// The one number here that changes on the scale of SECONDS, where everything around it changes
	// on the scale of days. Read from the file it is current; read from a sighting a minute old it
	// is a minute old, and a reader choosing between companions should treat it as what it is —
	// advisory. The authority is the refusal: a companion that is full says so when asked, and
	// that answer is never stale.
	Waiting int `json:"waiting,omitempty"`
	// Handling is whether a piece of handed-over work is running right now.
	//
	// Separate from Waiting because they are separate facts, and separate from the state a
	// dashboard derives because that is read off the session a person attaches to — and handed-over
	// work runs in conversations of its own. Without this a companion in the middle of somebody
	// else's request is indistinguishable from one with nothing to do.
	//
	// Wrong in one direction only, by construction: a daemon that loses track of a piece clears
	// this rather than leaving it set. Saying "free" when busy costs an asker a wait it did not
	// expect; saying "busy" forever would push every asker away from a companion that is fine.
	Handling bool   `json:"handling,omitempty"`
	PID      int    `json:"pid"`
	Started  string `json:"started"` // RFC3339
	// Host and Addr say WHERE this is running. Everything in one config directory is on one
	// machine, so on a laptop they are the same for every entry and read as noise — until you are
	// looking at three browser tabs forwarded from three hosts over ssh, which is the arrangement
	// this whole split exists for. Then the only thing telling them apart is this.
	Host string `json:"host,omitempty"`
	Addr string `json:"addr,omitempty"`
	// State is what this companion is doing, as the daemon that IS it says so: "waiting" on a
	// person, "working" on a turn, or "idle".
	//
	// It is here because it has to TRAVEL. A console dials the companions in its own directory and
	// works their state out from the answer; nothing dials the ones on other machines, so a roster
	// used to show them as "elsewhere" — a place, in a column about what things are doing. Gossip
	// already carries a sighting every round, which is exactly where Cassandra puts a node's
	// application state, and the record is signed, so a relay can carry this and cannot invent it.
	//
	// It is a SIGHTING, never a live reading: it was true when the record was written, and how long
	// ago that was travels beside it. A screen that shows one without the other is claiming to know
	// something it cannot.
	State string `json:"state,omitempty"`
	// Account is the OS user this daemon runs as, and with Host it names the INSTANCE a companion
	// belongs to.
	//
	// The pair is the unit everything else here is scoped to: two accounts on one machine read two
	// config directories, enforce two policies, keep two session stores, and neither can write the
	// other's files. A roster that said only the host would put two people's companions on one line
	// of provenance and imply they are interchangeable.
	Account string `json:"account,omitempty"`
	// Version is the build this daemon is running, which is not always the build the console
	// reading it is. Upgrading replaces the binary and leaves every daemon already running on the
	// old one until somebody restarts it — so a console showing only its own version answers a
	// question nobody asked, and the one they did ask ("why does this companion not have the thing
	// I just shipped") has no answer on the screen at all.
	Version string `json:"version,omitempty"`
	// Live is filled in by List, not by the daemon: a file cannot say whether the process that
	// wrote it is still there. Only a dial can.
	Live bool `json:"-"`
	// Asking is what the daemon is blocked on, when it is. Also from List, and for the same
	// reason: it is in the daemon's memory, so the dial that proves it alive asks while it is
	// there. nil when nothing is pending or the daemon is not answering.
	Asking *Waiting `json:"-"`
	// Doing is what a still-running tool last reported, and comes back on the same dial as Asking.
	// The pair is the whole of "what is happening in there right now": one says it has stopped and
	// needs a person, the other says it has not.
	Doing string `json:"-"`
	// Permission is the approval mode it is on now. Not in the file either: the mode changes at
	// runtime, so a record written at startup would be the mode it USED to be on.
	Permission string `json:"-"`
	// Backend is the base URL its LLM requests go to now, and is here for exactly the same reason:
	// the console can redirect it mid-run, so a record written at startup would name the endpoint
	// it USED to talk to.
	Backend string `json:"-"`
	// User is what this companion calls the person, when a plugin has renamed them. Not in the
	// file, for the same reason: a plugin can set it on any turn.
	User string `json:"-"`
	// Model is the model it is on now, here for the same reason and read from the same probe: the
	// listing already asks each companion how it is doing, and this rides that answer rather than
	// costing a second question or a walk through the workspace's session metadata.
	Model string `json:"-"`
}

// SessionFile is where a daemon records what it is driving.
func SessionFile(socketPath string) string { return socketPath + ".session" }

// Publish records the daemon and returns a function that removes the record.
// recordMu serialises read-modify-write on a daemon's own record.
//
// Three writers reach it from three goroutines of the same process: the queue's depth (Announce,
// from the drain), the session (Moved, from a serve goroutine), and the initial write. Each reads
// the whole record, changes one field and writes it back, so two of them overlapping loses one of
// the changes — and the losing field is whichever finished first, silently.
//
// Package-level rather than per-daemon: one process serves one socket, the sections it guards are
// microseconds long, and a test running several daemons at once loses nothing by taking turns.
var recordMu sync.Mutex

// writeRecord replaces a daemon's record in one step.
//
// Written to a temporary file and renamed, because the readers are POLLING it: os.WriteFile
// truncates and then writes, so a reader that lands in between gets an empty or half-written file
// and reports the companion as unreadable — a console blinking a daemon out of existence every
// time its queue depth changed. Rename is atomic on both platforms this ships to.
func writeRecord(socketPath string, in Info) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	f := SessionFile(socketPath)
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, f); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func Publish(socketPath, workdir, sid string, id Identity) (func(), error) {
	// Host(), not os.Hostname(): one spelling of this machine's name enters the system here and
	// nothing downstream has to normalise. A record written with the raw name and a member built
	// from the lowercased one compare unequal, and an election over the two decides that the
	// companion it just elected is not on any row it can see.
	host := Host()
	recordMu.Lock()
	err := writeRecord(socketPath, Info{
		Socket: socketPath, Workdir: workdir, Session: sid,
		Name: id.Name, Role: id.Role, Team: id.Team, Hub: id.Hub, Can: id.Can, Does: id.Does,
		PID: os.Getpid(), Started: time.Now().UTC().Format(time.RFC3339),
		Host: host, Addr: primaryAddr(), Account: account(), Version: version.Version,
	})
	recordMu.Unlock()
	if err != nil {
		return func() {}, fmt.Errorf("daemon: publishing: %w", err)
	}
	return func() { os.Remove(SessionFile(socketPath)) }, nil
}

// Announce updates the one part of a published record that changes while the daemon runs: how much
// work is waiting.
//
// Read-modify-write on the daemon's own record, which only it writes. Not a second publishing path:
// everything else in there is fixed when the daemon starts, and threading a number that changes
// every few seconds through the call that writes the fixed parts would make every caller of it
// pass something it does not know.
//
// A missing record is not an error. The daemon is either not published yet or on its way out, and
// in both cases the number describes nothing anybody can act on.
func Announce(socketPath string, waiting int, handling bool) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return nil
	}
	if in.Waiting == waiting && in.Handling == handling {
		return nil // nothing changed; do not rewrite a file readers are polling
	}
	in.Waiting, in.Handling = waiting, handling
	return writeRecord(socketPath, in)
}

// NoteState records what this companion is doing, in its own record.
//
// Read-modify-write on the daemon's own file, exactly like Announce, and written only when it
// CHANGED: readers poll this file, and rewriting it every few seconds to say the same thing would
// wake every one of them for nothing.
func NoteState(socketPath, state string) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return nil
	}
	if in.State == state {
		return nil
	}
	in.State = state
	return writeRecord(socketPath, in)
}

// Moved rewrites which conversation the published record names.
//
// Read-modify-write on the daemon's own record, exactly like Announce: everything else in there is
// fixed at startup, and the session is now the second thing that is not. It is the record, and not
// a message, that every reader believes — the fleet row, the console's withClient, an attaching
// terminal — so this write IS the move as far as anything outside the process is concerned.
func Moved(socketPath string, sid session.SessionID) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return err
	}
	if in.Session == string(sid) {
		return nil // do not rewrite a file readers are polling to say what it already says
	}
	in.Session = string(sid)
	return writeRecord(socketPath, in)
}

// account is the OS user this process runs as, or empty when it cannot be read — empty rather than
// a guess, since the whole value of the name is that it matches what somebody sees in a shell.
func account() string {
	u, err := osuser.Current()
	if err != nil {
		return ""
	}
	return u.Username
}

// primaryAddr is the address another machine would reach this one at, best effort.
//
// The first routable IPv4 on an interface that is up. Not a guarantee — a host with several NICs
// has several answers and this picks one — which is why it travels beside the hostname rather than
// instead of it. Empty when there is nothing routable, which is the honest answer for a laptop on
// no network.
func primaryAddr() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, in := range ifaces {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aerr := in.Addrs()
		if aerr != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

// Published reads what a daemon published.
func Published(socketPath string) (Info, error) {
	b, err := os.ReadFile(SessionFile(socketPath))
	if err != nil {
		return Info{}, fmt.Errorf("daemon: nothing published at %s — is a daemon running there? %w",
			SessionFile(socketPath), err)
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		return Info{}, fmt.Errorf("daemon: the record at %s is unreadable: %w", SessionFile(socketPath), err)
	}
	if strings.TrimSpace(in.Session) == "" {
		return Info{}, fmt.Errorf("daemon: the record at %s names no session", SessionFile(socketPath))
	}
	return in, nil
}

// PublishedSession is Published narrowed to the session id, for callers that want only that.
func PublishedSession(socketPath string) (string, error) {
	in, err := Published(socketPath)
	return in.Session, err
}

// probeTimeout bounds each half of a liveness probe: how long to wait for a connect, and how long
// to wait for the answer. Generous for a local unix socket answering from memory (a healthy daemon
// replies in well under a millisecond) and short enough that a listing stays usable when one
// process is wedged — which is exactly when somebody runs it.
const probeTimeout = 700 * time.Millisecond

// Find resolves one published socket under configDir, without probing anything.
//
// Separate from List because the two questions are different. A dashboard asks "what is out there,
// and which of them are alive?", which costs a dial to each. Delivering ONE steer asks "is this
// path one somebody published?", and answering that by probing every daemon on the machine makes a
// keystroke wait on an unrelated wedged process — the cost lands on the one action where a person
// is watching the cursor.
//
// Matched against the published set rather than parsed from the parameter: the path arrives from a
// page, and a path from a page must not become a path this process dials.
func Find(configDir, socket string) (Info, error) {
	socks, err := filepath.Glob(filepath.Join(configDir, "daemon-*.sock"))
	if err != nil {
		return Info{}, fmt.Errorf("daemon: listing: %w", err)
	}
	for _, s := range socks {
		if s != socket {
			continue
		}
		in, perr := Published(s)
		if perr != nil {
			return Info{}, perr
		}
		in.Socket = s
		return in, nil
	}
	return Info{}, fmt.Errorf("no daemon at %s — it is not one of the %d published under %s",
		socket, len(socks), configDir)
}

// List returns every daemon that has published under configDir, newest first.
//
// Each is DIALLED, because the file cannot say whether anybody is home: a daemon killed with
// SIGKILL leaves both the socket and the record behind, and a list that showed those as running
// would send a viewer to a dead endpoint. A dead one is still listed — knowing a workspace has a
// corpse is more useful than the entry silently missing — but it is marked.
func List(configDir string) ([]Info, error) {
	socks, err := filepath.Glob(filepath.Join(configDir, "daemon-*.sock"))
	if err != nil {
		return nil, fmt.Errorf("daemon: listing: %w", err)
	}
	out := make([]Info, len(socks))
	for i, s := range socks {
		in, err := Published(s)
		if err != nil {
			// A socket with no readable record: still worth showing, because something is there.
			in = Info{Socket: s, Workdir: "(unknown — no record)"}
		}
		in.Socket = s
		out[i] = in
	}
	out = Probe(out)
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out, nil
}

// Probe fills in the half of each Info only a dial can answer: liveness, and what the daemon is
// blocked on or doing right now. Split out of List so a listing whose RECORDS came another way —
// the web console's fleet path reads them off the roster door — pays the same dial and reads the
// same truth, instead of trusting a snapshot for the one thing snapshots cannot say.
func Probe(out []Info) []Info {
	// Probed in parallel. Serially, a listing costs the SUM of every daemon's latency and one
	// wedged process delays every entry after it — and the reason to run a listing is usually that
	// something is wrong. In parallel it costs the slowest one, which probeTimeout bounds.
	var wg sync.WaitGroup
	for i := range out {
		s, sid := out[i].Socket, out[i].Session
		out[i].Live, out[i].Asking, out[i].Doing = false, nil, ""
		wg.Add(1)
		go func(i int, s string, sid string) {
			defer wg.Done()
			// The dial that proves it alive also asks what it is waiting for: two questions, one
			// connection, and the second is free at the point the first is being answered.
			cl, derr := dialProbe(s, probeTimeout, probeTimeout)
			if derr != nil {
				return
			}
			defer cl.Close()
			out[i].Live = true
			if sid == "" {
				return
			}
			if st, serr := cl.Status(sid); serr == nil {
				out[i].Asking, out[i].Doing = st.Asking, st.Doing
				out[i].Permission, out[i].User = st.Permission, st.User
				out[i].Backend, out[i].Model = st.Backend, st.Model
			}
			// A daemon too old to know the method answers with an error naming what it does
			// accept. That is a version skew, not a fault: it is alive, and everything else about
			// it is still true. A TIMEOUT lands here too, and means the same thing for the entry:
			// alive, and not saying.
		}(i, s, sid)
	}
	wg.Wait()
	return out
}
