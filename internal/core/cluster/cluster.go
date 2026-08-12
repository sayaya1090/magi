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
	"fmt"
	"sort"
	"strconv"
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

// MaxDoes bounds how many capability names travel with one member.
//
// A member list is exchanged by every daemon every minute and it grows with the cluster, so
// anything per-member is paid N times by N machines. Twelve names is enough for a roster line to
// be worth reading and small enough that a workspace with two hundred skills cannot make the
// exchange expensive for everybody else.
const MaxDoes = 12

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
	// State is what that companion was doing when it was last seen: "waiting", "working", "idle".
	//
	// A sighting and not a reading — see daemon.Info.State. Signed with the rest, because a peer
	// that could rewrite it could tell everybody a machine's companions are idle and take their
	// work, or busy and starve them of it.
	State string `json:"state,omitempty"`
	// Account is the OS user the daemon runs as. With Host it names the instance: two accounts on
	// one machine are two config directories, two policies and two session stores, and a roster
	// that said only the host would imply their companions are interchangeable.
	Account string `json:"account,omitempty"`
	// Workdir is where it works, which is what tells two checkouts of one repo apart.
	Workdir string `json:"workdir,omitempty"`
	// Can is how many things it advertises being able to do: its written procedures plus the tool
	// servers it can reach. A crude number, and it is used for exactly one thing — see Speaker.
	Can int `json:"can,omitempty"`
	// Does NAMES those things, up to MaxDoes of them. Not descriptions: a name is enough to pick a
	// companion out of a roster, and what each one actually means is fetched from the machine that
	// has it when somebody wants to know (`magi --about`).
	//
	// The same split the whole package keeps — what a companion can be ASKED to do travels, and
	// everything heavier stays where it is. Descriptions on the wire would be a hundred bytes per
	// skill per member on every exchange, every minute, to fill a roster line that shows names.
	//
	// A SAMPLE when there are more than MaxDoes, which is why Can is carried separately rather
	// than being len(Does). Both are worked out in one place at one moment, so they cannot come to
	// disagree about a workspace.
	Does []string `json:"does,omitempty"`
	// Waiting is how much handed-over work that companion had not started when it was last seen.
	//
	// Never borrowed from an older sighting, unlike the capabilities beside it: those describe what
	// a companion IS and survive being seen twice, and this describes a moment. An old queue depth
	// carried onto a fresh record would be a number nobody ever observed together with the rest of
	// the row.
	Waiting int `json:"waiting,omitempty"`
	// Handling is whether it was in the middle of a piece of handed-over work when last seen.
	// Never borrowed either, and for the same reason.
	Handling bool `json:"handling,omitempty"`
	// Seen is when somebody last had it answer. Not when this entry was written: an entry copied
	// from a third companion carries the sighting it describes, or a rumour passed along twice
	// would look newer than the fact it came from.
	Seen time.Time `json:"seen"`
	// By is the public key of the machine this record describes, and Sig is that machine's
	// signature over everything above.
	//
	// # Why a record and not a message
	//
	// Gossip is transitive: what one machine sends includes what it heard about a third. Signing
	// the MESSAGE would prove only "this peer said this", so an admitted peer could invent a
	// companion on somebody else's host — or quietly change where an existing one answers — and it
	// would arrive indistinguishable from the truth. What has to be checkable is the RECORD, by
	// the machine it is about, which is the only party that can say where its own companions are.
	//
	// The key itself and not its fingerprint, because a fingerprint is a hash and a hash cannot
	// verify a signature. The fingerprint the admitted list compares is derived from this.
	By  string `json:"by,omitempty"`
	Sig string `json:"sig,omitempty"`
}

// Signable is the bytes a machine signs to vouch for one of its own records.
//
// Written out field by field rather than by marshalling the struct: JSON key order is stable in Go
// today and is not a promise, and a signature that depends on how a library felt like ordering a
// map is a signature that stops verifying on an upgrade. Length-prefixed, so two fields cannot be
// slid into one another — "host=a, socket=bc" and "host=ab, socket=c" have to be different bytes.
//
// # What is signed is what does not move
//
// Identity and capabilities: where a companion answers, what it calls itself, and what it says it
// can do. Those are the fields that decide where work is sent and who a caller is talking to, and
// nothing between here and there is allowed to rewrite them.
//
// Seen is deliberately NOT signed, and neither are Waiting and Handling. Merge clamps a sighting
// from the future — a clock running fast is not news — and those two are moments that are never
// carried from an older entry. Signing a field the design rewrites would break the signature on
// exactly the machines the rewrite exists for, which is the loudest possible way to be wrong about
// nothing.
//
// The cost is stated plainly: freshness is a relay's claim, not the owner's. An admitted peer can
// keep a companion that HAS existed looking alive for as long as it keeps repeating the record.
// What it cannot do is invent one, move one to a socket of its choosing, or change what one claims
// to be — which is the class of thing a roster is acted on for.
func Signable(m Member) []byte {
	var b strings.Builder
	put := func(s string) {
		fmt.Fprintf(&b, "%d:%s|", len(s), s)
	}
	put(strings.ToLower(m.Host))
	put(m.Socket)
	put(m.Name)
	put(m.Role)
	put(m.Team)
	put(m.Account)
	put(m.State)
	put(strconv.FormatBool(m.Hub))
	put(m.Workdir)
	put(strconv.Itoa(m.Can))
	put(strings.Join(m.Does, "\x00"))
	return []byte(b.String())
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
		// One spelling of a hostname, established here because this is the one place every member
		// passes through — Mine, what arrives over the wire, and what was on disk all come through
		// Merge. Fixing it at the funnel rather than at each comparison is what makes Key work:
		// key is host plus socket, so "buildbox" and "BuildBox" were two companions, both offered
		// in every roster, with work going to whichever the model happened to pick.
		//
		// Reachable without anybody doing anything odd. os.Hostname() reports different casing on
		// macOS depending on which of its several host names was last set, so one reboot did it —
		// and this file already asked "is this us" two different ways, one case-sensitive and one
		// not, which is how a member could be recorded and then never displayed.
		m.Host = strings.ToLower(m.Host)
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
//
// Except from a record its owner signed. A signature is a statement about the fields it covers, so
// a signed record is complete by definition: what it does not say is not missing, it is absent.
// Borrowing into one would also make the bytes stop matching the signature, and this machine
// relays what it stores — so the improvement would arrive at the next machine as a forgery.
func fillFrom(keep, other Member) Member {
	if keep.Sig != "" {
		return keep
	}
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
	if keep.Account == "" {
		keep.Account = other.Account
	}
	// State is NOT borrowed. Everything else in here describes what a companion IS and survives
	// being seen twice; this describes a moment, and an old "working" carried onto a fresh sighting
	// would be a claim nobody made — the same rule Waiting and Handling already follow.
	// Capabilities are borrowed as a pair or not at all. The names from one sighting and the count
	// from another would give a member advertising three things out of nine when nobody ever saw
	// it that way.
	if len(keep.Does) == 0 && keep.Can == 0 {
		keep.Does, keep.Can = other.Does, other.Can
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

// Speaker returns the companion that answers for a team right now.
//
// # Why a team needs one at all
//
// Addressing a team is addressing whoever speaks for it, and a hub is also the only companion
// allowed to split work it was given across its own team. Both stop working the moment the declared
// hub is not there, and until now that was the end of it: the team became unaddressable and its
// members lost the one thing that let them share work out.
//
// # Elected by everybody, agreed by nobody
//
// There is no vote and no message. Every companion computes this from its own membership list, so
// they agree exactly as far as their lists agree — which after a round of merging is usually all
// the way. Nothing is stored, so nothing has to be un-stored when the declared hub returns: it is
// recomputed each time it is asked, and the original resumes the moment it is seen again.
//
// That is deliberately not consensus. Two halves of a partition would each elect their own, and
// this does nothing about it — split brain is out of scope, and pretending otherwise with a quorum
// nobody can reach would be worse than the honest gap.
//
// # Declaring is a preference, not a claim
//
// An earlier version treated `hub = true` as a claim and refused when two companions made it: a
// person's mistake, said the comment, with a one-line fix in a file. It was the wrong shape, and
// the systems that solve this say so. MongoDB never lets anybody DECLARE primary — a primary is
// always elected, and what config carries is a `priority` that makes a member preferred, which two
// members may share without conflicting because the election still returns one. Cassandra sidesteps
// the question by having no leader at all.
//
// So this always elects, and a declaration only moves a companion to the front of the queue. Two
// declarations are then two candidates rather than a conflict, and the outcome nobody wants — a
// team that cannot be addressed because somebody typed one word twice — cannot happen.
//
// acting says the answer is an election among equals rather than a declared preference. It matters to the one elected —
// it is what lets it hand work on — and to anybody reading a roster, because "acting" is the word
// that says the companion somebody expected is missing.
func Speaker(ms []Member, team string, now time.Time) (who Member, acting, ok bool) {
	team = strings.TrimSpace(team)
	if team == "" {
		return Member{}, false, false
	}
	var fresh, preferred []Member
	for _, m := range ms {
		if !strings.EqualFold(m.Team, team) || !m.Fresh(now) {
			continue
		}
		fresh = append(fresh, m)
		if m.Hub {
			preferred = append(preferred, m)
		}
	}
	// The preferred, when there are any. Two of them is a field of two candidates, not a conflict:
	// the election below returns one either way.
	acting = len(preferred) == 0
	if !acting {
		fresh = preferred
	}
	if len(fresh) == 0 {
		return Member{}, false, false
	}
	// Then the one that can do the most, and only then the lowest key.
	//
	// The hub is where team-addressed work lands and the only companion allowed to split it up. A
	// hub that can do little does little: it forwards, which costs a hop, a second paraphrase of
	// the request, and the roundtrip — for nothing. The one already able to do the work should be
	// the one holding it.
	//
	// This is a value that MOVES, which the first version of this comment argued against. The
	// argument was about values that move by the SECOND — least busy, longest running — where a
	// hub changes because somebody started a build. A capability count changes when a skill is
	// written, which is days apart, so companions computing this alone still agree; and when their
	// counts disagree, one merge settles it, exactly like every other field here.
	//
	// Crude, and knowingly: thirty small skills outrank three deep ones. It is a tie-break, not a
	// decision — a declared preference still beats it — and the alternative it replaces carried no
	// signal at all.
	best := fresh[0]
	for _, m := range fresh[1:] {
		switch {
		case m.Can > best.Can:
			best = m
		case m.Can == best.Can && m.Key() < best.Key():
			best = m
		}
	}
	return best, acting, true
}
