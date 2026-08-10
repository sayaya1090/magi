// Package cluster is who else is out there, as a thing that decays rather than a thing declared.
//
// # Why this is not configuration
//
// A companion written into config.toml is a DEPENDENCY: it says "I need this to exist". It never
// goes stale on its own, so somebody has to remember to delete it, and until they do, every screen
// and every roster keeps offering a companion that has not run since March.
//
// Membership is the other thing. It says "this was there a minute ago", which is an observation and
// not a requirement. The difference between the two is exactly that one of them EXPIRES — and that
// is why nothing here is written to a config file. It lives beside the daemon records, which are
// already runtime facts a process writes when it starts and drops when it stops.
//
// One machine has needed none of this: every companion publishes to one directory and every
// companion reads it, so the roster is exact and free. This is that same idea reaching past the
// machine, where there is no shared directory — so instead of reading, they tell each other.
//
// # What travels, and what does not
//
// Identity travels: a name, a role, a team, a host, the socket it answers on. How to REACH it does
// not. A reach is a command line, and a command line arriving over the network is arbitrary code
// this process would later run — which is what join refuses, and the refusal stands.
//
// So a hostname is data and a command is code: `ssh buildbox magi --mcp design` is assembled HERE,
// from this machine's own template, with nothing but the host taken from what arrived. A member
// entry cannot make anybody run anything it chose.
//
// # There is no allowlist, because there is already ssh
//
// An earlier draft had the operator declare which hosts may be in a cluster. It was wrong twice:
// it is a dependency written in config, which is the thing this package exists to avoid, and it
// bought nothing — joining means reaching a member, reaching means ssh, and anybody with a shell on
// a cluster machine can do far worse than add a row here. The boundary is the one that already
// governs every other part of this: the user who owns these files.
//
// # Merging, and whose clock decides
//
// Everybody pulls from everybody they know and keeps the fresher of two sightings, so a cluster
// converges without a leader, a registry, or anything in the middle that can be down.
//
// The timestamps come from other machines, and clocks disagree. A skew of minutes matters when the
// threshold is five, so a sighting from the future is clamped to now rather than trusted: the worst
// a wrong clock can do is make a member look staler than it is, which shows up as "unseen" and gets
// corrected by the next direct sighting. The reverse — trusting a clock running fast — would keep a
// dead companion looking alive forever, and nothing later could correct it.
package cluster

import (
	"sort"
	"strings"
	"time"
)

// Unseen is how long without a sighting before a member is shown as out of touch, and Forget is how
// long before it is dropped altogether.
//
// Two thresholds rather than one, because the two mistakes are not symmetrical. Dropping too early
// loses a companion that was restarting, and it comes back only when somebody sights it again —
// meanwhile work that should have gone to it goes somewhere worse. Keeping too long offers a
// companion that is not there, and every attempt to use it costs a turn. So: say so quickly, forget
// slowly.
const (
	Unseen = 5 * time.Minute
	Forget = time.Hour
)

// Member is one companion somebody has seen.
type Member struct {
	// Host is the machine it runs on, and Socket is the path it answers on THERE. The pair is the
	// identity: a name can be changed and two machines can hold the same one, but no two
	// companions share a socket on one host.
	Host   string `json:"host"`
	Socket string `json:"socket"`
	// Name, Role, Team and Hub are what it calls itself — the same fields a daemon publishes
	// locally, carried across so a remote companion reads the same as a local one everywhere.
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
	Team string `json:"team,omitempty"`
	Hub  bool   `json:"hub,omitempty"`
	// Workdir is where it works, which is what tells two checkouts of one repo apart.
	Workdir string `json:"workdir,omitempty"`
	// Seen is when somebody last had it answer. Not when this entry was written: an entry copied
	// from a third companion carries the sighting it describes, or a rumour passed along twice
	// would look newer than the fact it came from.
	Seen time.Time `json:"seen"`
}

// Key identifies a companion across the cluster.
func (m Member) Key() string { return m.Host + "\x00" + m.Socket }

// Fresh reports whether this member has been sighted recently enough to be worth offering work to.
func (m Member) Fresh(now time.Time) bool { return now.Sub(m.Seen) < Unseen }

// Merge folds sightings into a list, keeping the later of any two of the same companion, and drops
// whatever nobody has seen for Forget.
//
// now is passed rather than read so a test drives the clock, and because every threshold here is
// relative to one instant: reading time.Now() twice inside would let a member expire between two
// lines of the same merge.
func Merge(have, heard []Member, now time.Time) []Member {
	by := map[string]Member{}
	for _, m := range append(append([]Member(nil), have...), heard...) {
		if m.Host == "" || m.Socket == "" {
			continue // no identity: there is nothing to merge it against, and nothing to reach
		}
		// A sighting from the future is a clock running fast, not news. Clamped rather than
		// dropped: the entry is still evidence the companion exists, and the worst this can do is
		// age it early — which the next direct sighting corrects.
		if m.Seen.After(now) {
			m.Seen = now
		}
		if prev, ok := by[m.Key()]; ok && prev.Seen.After(m.Seen) {
			// The older sighting loses, but its IDENTITY is not thrown away: a companion that has
			// gone quiet still has the name and role somebody recorded for it, and an entry
			// arriving with those blank must not blank them.
			by[m.Key()] = fillFrom(prev, m)
			continue
		}
		if prev, ok := by[m.Key()]; ok {
			by[m.Key()] = fillFrom(m, prev)
			continue
		}
		by[m.Key()] = m
	}
	out := make([]Member, 0, len(by))
	for _, m := range by {
		if now.Sub(m.Seen) >= Forget {
			continue
		}
		out = append(out, m)
	}
	// Host then socket: a stable order, so a file written twice with no change is the same bytes
	// and a diff of it shows what actually moved.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Socket < out[j].Socket
	})
	return out
}

// fillFrom takes keep whole and borrows any identity field it is missing from other.
func fillFrom(keep, other Member) Member {
	if keep.Name == "" {
		keep.Name = other.Name
	}
	if keep.Role == "" {
		keep.Role = other.Role
	}
	if keep.Team == "" {
		keep.Team = other.Team
	}
	if keep.Workdir == "" {
		keep.Workdir = other.Workdir
	}
	// Hub is a declaration, and a companion that has stopped declaring it has stopped being one.
	// Only borrowed onto an entry that carries no identity at all, which is what a bare sighting
	// is — otherwise a demoted hub would be promoted back by its own history.
	if !keep.Hub && other.Hub && keep.Name == "" {
		keep.Hub = true
	}
	return keep
}

// Reach is the command that opens an MCP conversation with a member, assembled HERE.
//
// This is the line the whole package is arranged around. What arrived over the network is a
// hostname; what runs is built from this machine's own template. A member entry can say where a
// companion is and can never say what to execute.
//
// local is this machine's name: a member on it needs no ssh, and going through one would fail on
// any host that cannot ssh to itself — which is most of them.
func Reach(m Member, local, self, magi string) (string, []string) {
	name := m.Name
	if name == "" {
		name = m.Socket // no declared name: address it by the thing that is unique
	}
	args := []string{"--mcp", name, "--mcp-as", self}
	if m.Host != "" && !strings.EqualFold(m.Host, local) {
		return "ssh", append([]string{m.Host, magi}, args...)
	}
	return magi, args
}
