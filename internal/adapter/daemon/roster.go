package daemon

import (
	"path/filepath"
	"time"
)

// The fleet through one socket: `roster` answers "who is out there" to a client that holds one
// control socket and nothing else.
//
// The daemon already owns both halves of the answer — this machine is the publish directory
// (exact, read live), other machines are the signed gossip sightings (an hour's decay) — and it
// already reads them together everywhere else (Known). What was missing was a door: the web
// console reads the directory and the logs itself, but a JVM plugin or an add-in holds a socket
// and cannot, and without this method its only view of the fleet was the one companion it dialed.
//
// Discovery only, on purpose. Command and conversation stay on each companion's OWN door — the
// same boundary that kept the transcript off the fleet door: a companion relaying another's
// conversation would flatten per-companion access and freshness into its own. A caller reads the
// roster here, then dials the row it wants.

// RosterRow is one companion as the roster door reports it.
//
// Its own wire shape rather than cluster.Member, for two reasons: a local row carries facts a
// sighting never has (the session id, a live dial), and Member's shape is signed — a field added
// for this door would ride the gossip wire and change what older daemons verify.
type RosterRow struct {
	Host    string `json:"host,omitempty"`
	Socket  string `json:"socket"`
	Name    string `json:"name,omitempty"`
	Role    string `json:"role,omitempty"`
	Team    string `json:"team,omitempty"`
	Hub     bool   `json:"hub,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	Account string `json:"account,omitempty"`
	// State is the vocabulary Info.State pins: "waiting" on a person, "working" on a turn, or
	// "idle". A badge that distinguishes "somebody is needed" from "busy" hangs on the first one.
	State   string `json:"state,omitempty"`
	Version string `json:"version,omitempty"`
	// PID, Addr and Started are record facts a local row carries so a consumer that used to read
	// the record itself (the web console) loses nothing by coming through the door instead.
	// Absent on sightings — they never travel the gossip wire.
	PID     int    `json:"pid,omitempty"`
	Addr    string `json:"addr,omitempty"`
	Started string `json:"started,omitempty"`
	// By is the public key of the machine a SIGHTING is signed by, so a consumer can grade its
	// trust the same way it would reading the sighting file itself. Local rows carry none — this
	// machine does not vouch to itself.
	By       string   `json:"by,omitempty"`
	Can      int      `json:"can,omitempty"`
	Does     []string `json:"does,omitempty"`
	Waiting  int      `json:"waiting,omitempty"`
	Handling bool     `json:"handling,omitempty"`
	// Session is the conversation this companion is in now — the cursorless entry a client needs
	// to open its transcript without a second round trip. Local rows only: a sighting does not
	// carry it, and a client could not subscribe across machines anyway.
	Session string `json:"session,omitempty"`
	// What the listing's own probe asked and got. These exist only in the running process — the
	// approval mode it is on, the backend its requests go to, the model the conversation is on —
	// and List already has them in hand when it builds this row. Dropping them here made a console
	// that reads the roster door (which is every console: the light listing prefers it) draw those
	// fields empty and then offer to change values it could not show.
	Permission string `json:"permission,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Model      string `json:"model,omitempty"`
	User       string `json:"user,omitempty"`
	// Live reports that a dial just proved somebody is listening. Local rows only; a sighting's
	// liveness is exactly what nobody here can check, which is what Sighting is for.
	Live bool `json:"live,omitempty"`
	// Sighting marks a row this machine did not measure: it arrived as a record another machine
	// signed. Visible, not commandable — its socket is a path on a machine this caller has no
	// door to.
	Sighting bool `json:"sighting,omitempty"`
	// AgeSeconds is how old the fact is: 0 for a live local read, and the time since the sighting
	// for a gossiped row. A screen that shows a state without its age is claiming to know
	// something it cannot (Info.State says the same).
	AgeSeconds int64 `json:"ageSeconds,omitempty"`
}

// buildRoster reads both halves the way Known does, but keeps them tellable-apart: local rows are
// measurements (dial, session, no age), elsewhere rows are sightings (age, no session).
func buildRoster(home string, now time.Time) []RosterRow {
	var rows []RosterRow
	if list, err := List(home); err == nil {
		for _, in := range list {
			rows = append(rows, RosterRow{
				Host: in.Host, Socket: in.Socket, Name: in.Name, Role: in.Role,
				Team: in.Team, Hub: in.Hub, Workdir: in.Workdir, Account: in.Account,
				State: in.State, Version: in.Version, Can: in.Can, Does: in.Does,
				Waiting: in.Waiting, Handling: in.Handling,
				PID: in.PID, Addr: in.Addr, Started: in.Started,
				Session: in.Session, Live: in.Live,
				Permission: in.Permission, Backend: in.Backend, Model: in.Model, User: in.User,
			})
		}
	}
	for _, m := range Elsewhere(home, now) {
		age := int64(now.Sub(m.Seen) / time.Second)
		if age < 0 {
			age = 0
		}
		rows = append(rows, RosterRow{
			Host: m.Host, Socket: m.Socket, Name: m.Name, Role: m.Role,
			Team: m.Team, Hub: m.Hub, Workdir: m.Workdir, Account: m.Account,
			State: m.State, Version: m.Version, Can: m.Can, Does: m.Does,
			Waiting: m.Waiting, Handling: m.Handling, By: m.By,
			Sighting: true, AgeSeconds: age,
		})
	}
	return rows
}

// answerRoster is the door. home is the directory the daemon's own socket lives in — the same
// place records are published and sightings are kept — threaded from the listener because it is a
// fact about the MACHINE, which no Engine method answers.
func answerRoster(home string) Response {
	if home == "" {
		// A daemon serving without a home (some tests) has no directory to read. An empty list
		// would say "you are alone", which is not known; say why instead.
		return Response{Err: "this daemon has no home directory to read a roster from"}
	}
	rows := buildRoster(home, time.Now())
	if rows == nil {
		rows = []RosterRow{} // an empty fleet is an answer; JSON null is a shrug
	}
	return Response{OK: true, Roster: rows}
}

// ProbeRoster asks the daemon at path for its roster, with the same bounded patience a liveness
// probe has. For a consumer walking candidate sockets (the web console's fleet path): a wedged
// daemon must cost one probeTimeout, not hang the listing.
func ProbeRoster(path string) ([]RosterRow, error) {
	cl, err := dialProbe(path, probeTimeout, probeTimeout)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	return cl.Roster()
}

// homeOf is the machine half's location, derived from the daemon's own socket path: records and
// sightings live beside the socket by construction (SocketPath joins them into one directory).
func homeOf(socketPath string) string {
	if socketPath == "" {
		return ""
	}
	return filepath.Dir(socketPath)
}
