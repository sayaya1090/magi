package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/cluster"
)

// Where the cluster's membership is kept, and how the two halves of "who is out there" meet.
//
// This machine already answers that question for itself: every daemon publishes a record here when
// it starts and removes it when it stops, and anybody can read the directory. That half needs no
// file — it IS the directory, and it is exact.
//
// The other half is companions on other machines, which no directory here can see. They arrive by
// being told, and what is told has to survive a restart or every reboot would leave a companion
// alone in a cluster it had joined. So it is written down — beside the records, not in config,
// because it decays and configuration does not (see internal/core/cluster).
//
// Read together, always. Two callers each asking one half is how a roster comes to disagree with
// the screen next to it.

// clusterFile is where sightings of other machines' companions are kept.
func clusterFile(configDir string) string { return filepath.Join(configDir, "cluster.json") }

// Host is what this machine calls itself, and is the half of a member's identity that says where.
//
// An unnameable host is "" rather than an error or a guess. Everything downstream treats it as a
// member with nowhere to be reached, which is the truth: a machine that cannot say its own name is
// one nothing else can address either.
func Host() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	// Lowercased, the same way cluster.Merge lowercases everything it stores, so "is this us" is
	// one comparison against one spelling wherever it is asked.
	return strings.ToLower(h)
}

// Mine is the companions published on THIS machine, as members, seen just now.
//
// Live ones only. A record whose daemon is gone is what SIGKILL leaves behind, and passing it on
// would put a companion into other machines' lists that this one already knows is not there —
// gossip's worst failure, because it takes an hour to expire and cannot be corrected by anybody
// who is further away than the process that could see the truth.
//
// The capability count comes off the record rather than being worked out here: it is counted by
// the daemon that owns the workspace, which is the only process that can see both halves of it.
//
// Signed on the way out — see Vouch. Here rather than at whichever code path happens to be sending:
// this is the one place a record about THIS machine is made, and a record that is signed only when
// it leaves by the door somebody remembered to change is a record that arrives unsigned, and is
// dropped, from the other three.
func Mine(configDir string, now time.Time) []cluster.Member {
	found, err := List(configDir)
	if err != nil {
		return nil
	}
	host := Host()
	out := make([]cluster.Member, 0, len(found))
	for _, in := range found {
		if !in.Live {
			continue
		}
		m := cluster.Member{
			Host: host, Socket: in.Socket, Name: in.Name, Role: in.Role,
			Team: in.Team, Hub: in.Hub, Workdir: in.Workdir,
			Can: in.Can, Does: in.Does, Waiting: in.Waiting, Handling: in.Handling, Seen: now,
		}
		out = append(out, m)
	}
	return Vouch(configDir, out)
}

// Known is everything this machine can say about who is out there: its own live companions, and the
// sightings of other machines' that it has been told about.
func Known(configDir string, now time.Time) []cluster.Member {
	return cluster.Merge(Mine(configDir, now), readMembers(configDir), now)
}

// Elsewhere is the half that is NOT on this machine: companions somebody told us about.
//
// Split out because the two halves are read differently by anything that lists agents — this
// machine's are rows built from a log and a dial, and these are rows built from a sighting. A
// caller that took Known and tried to tell them apart by hostname would be doing this again,
// slightly differently, in a place where getting it wrong shows a remote companion as stopped.
func Elsewhere(configDir string, now time.Time) []cluster.Member {
	host := Host()
	all := cluster.Merge(readMembers(configDir), nil, now)
	out := make([]cluster.Member, 0, len(all))
	for _, m := range all {
		if host != "" && strings.EqualFold(m.Host, host) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// LearnMembers folds what somebody else knows into what this machine has written down, and returns
// the whole picture.
//
// Only the FILE is written, and this machine's own companions are never put in it: they are read
// from the published records every time, so a copy here could only ever be a second answer that
// goes stale the moment a daemon stops.
func LearnMembers(configDir string, heard []cluster.Member, now time.Time) ([]cluster.Member, error) {
	host := Host()
	var theirs []cluster.Member
	// Checked before anything is believed, and here because this is the ONE door records from
	// another machine come through: every exchange, every relay, every restore from a peer ends up
	// in this function. A check at the call sites would be four checks, and the fifth call site
	// would be the one somebody adds next month.
	for _, m := range Vouched(configDir, heard, nil) {
		if host != "" && strings.EqualFold(m.Host, host) {
			continue // ours, and the directory is the authority on ours
		}
		theirs = append(theirs, m)
	}
	merged := cluster.Merge(readMembers(configDir), theirs, now)
	if err := writeMembers(configDir, merged); err != nil {
		return nil, err
	}
	return cluster.Merge(Mine(configDir, now), merged, now), nil
}

func readMembers(configDir string) []cluster.Member {
	b, err := os.ReadFile(clusterFile(configDir))
	if err != nil {
		return nil
	}
	var out []cluster.Member
	if json.Unmarshal(b, &out) != nil {
		// A corrupt file is not a reason to refuse to work. The cluster rebuilds itself from one
		// exchange with anybody still reachable, which is the property the whole design is for.
		return nil
	}
	return out
}

func writeMembers(configDir string, ms []cluster.Member) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	// Indented and sorted (Merge sorts): this is a file a person will open when a companion is
	// missing, and a diff of it should show what actually moved.
	b, err := json.MarshalIndent(ms, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(clusterFile(configDir), append(b, '\n'), 0o644)
}
