package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/cluster"
)

// Joining a cluster, and keeping one.
//
// # One call does both
//
// `magi --members` folds a member list on stdin into what this machine knows and prints the result.
// That is the entire transport. Joining is that call made once against a seed; staying joined is
// the same call made again later. Two things that are really one act stay one piece of code, which
// is the only way they cannot drift.
//
// # Why ssh and nothing else
//
// The far side needs a magi binary and shell access, and shell access is the boundary this whole
// design already rests on — mcpserve makes the same argument. No port, no listener, no token, and
// nothing for an operator to secure that ssh has not already secured.
//
// # What is trusted from over there
//
// Identity, and not a way to reach anybody. What comes back is names and hostnames; the ssh line
// that reaches a member is assembled here from this machine's own template (cluster.Reach). A
// member entry can say where a companion is and can never say what to run.

// countCan is what a workspace advertises being able to do: how many things, and what the first
// few are called — its written procedures plus the tool servers it can reach.
//
// The same two things `about` shows a companion asking what somebody else does, counted rather than
// listed — so the number a hub election turns on is the number a reader would arrive at from the
// advertisement. Two derivations of "how much can it do" would disagree the first time one changed.
//
// Called once, where a daemon publishes itself. Nobody else counts: the number travels on the
// record and then over the wire, so every reader on every machine has the one this process worked
// out, rather than a second answer derived from files it cannot see.
func countCan(store *jsonl.Store, workdir string) (int, []string) {
	if workdir == "" {
		return 0, nil
	}
	reader := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	var names []string
	for _, sk := range reader.Skills(workdir) {
		names = append(names, sk.Name)
	}
	names = append(names, reachableServers(workdir)...)
	// The count is of everything; the names are the first few. Returned together, from one read of
	// one workspace, so the number and the list cannot describe different moments.
	n := len(names)
	if len(names) > cluster.MaxDoes {
		names = names[:cluster.MaxDoes]
	}
	return n, names
}

// exchangeMembers is the `--members` half: read theirs, write ours.
func exchangeMembers(in io.Reader, out, errOut io.Writer, configDir string) int {
	heard := readMemberList(in, errOut)
	known, err := daemon.LearnMembers(configDir, heard, time.Now())
	if err != nil {
		// Say it, and still answer. The caller asked who is out there; failing to record what they
		// told us is not a reason to leave them knowing nothing.
		fmt.Fprintln(errOut, "magi: could not record what was heard:", err)
		known = daemon.Known(configDir, time.Now())
	}
	b, err := json.Marshal(known)
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	if _, err := out.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	return 0
}

// readMemberList takes a member list off a reader, or nothing.
//
// Nothing is the ordinary case: `magi --members` run by hand at a terminal has an idle stdin, and
// waiting on it would hang. So an empty or unparseable body is a caller that only wanted to ask,
// which is a legitimate half of a symmetric exchange.
func readMemberList(in io.Reader, errOut io.Writer) []cluster.Member {
	if in == nil {
		return nil
	}
	b, err := io.ReadAll(in)
	if err != nil || len(b) == 0 {
		return nil
	}
	var ms []cluster.Member
	if err := json.Unmarshal(b, &ms); err != nil {
		fmt.Fprintln(errOut, "magi: ignoring what arrived on stdin:", err)
		return nil
	}
	return ms
}

// joinTheCluster trades member lists with a machine and joins whatever it is part of.
//
// The seed is named once, by a person, at the moment a companion is created. Nothing about it is
// written down afterwards — which is the point: a seed recorded in config would be a dependency,
// and a cluster whose members depend on one machine has a machine that cannot be turned off.
func joinTheCluster(out, errOut io.Writer, configDir, host string) int {
	heard, err := sshMembers(context.Background(), host, daemon.Known(configDir, time.Now()), false)
	if err != nil {
		fmt.Fprintf(errOut, "magi: %s did not answer: %v\n", host, err)
		fmt.Fprintf(errOut, "      It needs magi on its PATH and this machine needs to be able to "+
			"`ssh %s`.\n", host)
		return 1
	}
	known, err := daemon.LearnMembers(configDir, heard, time.Now())
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	fmt.Fprintf(out, "Joined through %s. %d companion(s) in the cluster:\n", host, len(known))
	for _, m := range known {
		fmt.Fprintf(out, "  %s\n", describeMember(m, time.Now()))
	}
	fmt.Fprintln(out, "\nNothing was written to any config. The list is kept beside the daemon "+
		"records and members drop out of it when nobody has seen them for an hour.")
	return 0
}

// sshMembers runs one exchange against another machine: our list goes over, theirs comes back.
//
// batch says nobody is at a keyboard. It matters more than it looks: without BatchMode ssh asks for
// a passphrase on the terminal, and a daemon has no terminal — the process would sit there holding
// the round open until the context killed it, every minute, forever. With it, a host that cannot be
// reached without a person says so immediately, which is a thing that can be reported. A person
// running --join-cluster by hand gets the prompt, because there is somebody to answer it.
func sshMembers(ctx context.Context, host string, mine []cluster.Member, batch bool) ([]cluster.Member, error) {
	// host reaches ssh's argv; a value beginning with '-' would be parsed as an option (see
	// relayTo's sshSafeArg) — for a gossip-driven join that is remote-influenced local command
	// execution, and for a hand-typed one it is a typo that should fail loudly, not run ssh with a
	// stray flag.
	if !sshSafeArg(host) {
		return nil, fmt.Errorf("refusing to ssh to %q: a host cannot begin with '-'", host)
	}
	body, err := json.Marshal(mine)
	if err != nil {
		return nil, err
	}
	// The remote binary is named as plain `magi`, found on their PATH. Not this machine's path to
	// it: the two are the same program and rarely the same install, and sending an absolute path
	// from here is how a join fails on a host where it happens to live somewhere else.
	var args []string
	if batch {
		args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
	}
	args = append(args, host, "magi", "--members")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = bytes.NewReader(body)
	if !batch {
		cmd.Stderr = os.Stderr
	}
	said, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var heard []cluster.Member
	if err := json.Unmarshal(said, &heard); err != nil {
		return nil, fmt.Errorf("answered with something that is not a member list: %w", err)
	}
	return heard, nil
}

// How a daemon keeps its picture of the cluster current.
//
// # Why a pull at all, when joining already told everybody
//
// It did not. A join is one exchange between two machines: C reaching A leaves A and C knowing each
// other and B — already in the cluster — knowing nothing about C. Nothing else in the design ever
// closes that gap. This is the piece that does, and without it a cluster is a set of pairs rather
// than a cluster.
//
// # Everybody pulls, so nothing has to push
//
// A round takes what this machine knows to somebody and brings back what they know. Both sides
// learn, because the exchange is symmetric — the same call a join makes. So there is no announcer,
// no registry, and nothing whose being down stops the rest from converging.
//
// # A few at a time, not everybody
//
// A round talks to gossipFanout hosts chosen at random rather than to all of them. That is what
// makes the cost independent of how big the cluster is: sixty machines would otherwise be sixty ssh
// handshakes a minute from each of sixty daemons. Random choice still reaches everyone quickly —
// what one machine learns is carried on by whoever it spoke to — and news crosses the cluster in
// rounds proportional to its logarithm, not its size.
//
// # The clock, against the thresholds
//
// A round a minute against five minutes before a member reads as out of touch: five chances to
// hear about somebody before anybody is told they are missing, and sixty before they are dropped.
// Jittered, because a rack of daemons started by one script would otherwise all reach out on the
// same second and go on doing it forever.
const (
	gossipEvery   = time.Minute
	gossipFanout  = 2
	gossipTimeout = 20 * time.Second
)

// tradeFunc is one exchange with one host. Injected so the loop can be tested without ssh.
type tradeFunc func(ctx context.Context, host string, mine []cluster.Member) ([]cluster.Member, error)

func sshTrade(ctx context.Context, host string, mine []cluster.Member) ([]cluster.Member, error) {
	return sshMembers(ctx, host, mine, true)
}

// gossipCluster keeps this machine's membership current until ctx ends.
//
// Daemon-only, for the reason the scheduler is: three terminals open in one repo would be three
// processes reaching out to the same hosts on the same clock, saying the same thing.
func gossipCluster(ctx context.Context, configDir string, trade tradeFunc, warn func(string)) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Complained-about hosts, so an unreachable machine is reported once rather than every minute
	// for as long as it is down. A log line a minute is a log nobody reads.
	quiet := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(rng, gossipEvery)):
		}
		gossipRound(ctx, configDir, trade, warn, rng, quiet)
	}
}

// jitter spreads a fixed period by ±25%.
func jitter(rng *rand.Rand, d time.Duration) time.Duration {
	quarter := int64(d) / 4
	return time.Duration(int64(d)-quarter) + time.Duration(rng.Int63n(2*quarter+1))
}

func gossipRound(ctx context.Context, configDir string, trade tradeFunc,
	warn func(string), rng *rand.Rand, quiet map[string]bool) {
	mine := daemon.Known(configDir, time.Now())
	for _, host := range pickHosts(otherHosts(mine, daemon.Host()), gossipFanout, rng) {
		rctx, cancel := context.WithTimeout(ctx, gossipTimeout)
		heard, err := trade(rctx, host, mine)
		cancel()
		if err != nil {
			if !quiet[host] && warn != nil {
				warn("cannot reach " + host + " to trade member lists: " + err.Error())
			}
			quiet[host] = true
			continue
		}
		if quiet[host] && warn != nil {
			warn(host + " is answering again")
		}
		delete(quiet, host)
		if _, lerr := daemon.LearnMembers(configDir, heard, time.Now()); lerr != nil && warn != nil {
			warn("could not record what " + host + " said: " + lerr.Error())
		}
	}
}

// otherHosts is the distinct machines in a member list, this one excluded.
//
// By host and not by member: several companions on one machine answer from one membership file, so
// asking it once per companion would be the same answer two or three times over one ssh each.
func otherHosts(ms []cluster.Member, local string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range ms {
		if m.Host == "" || (local != "" && strings.EqualFold(m.Host, local)) || seen[m.Host] {
			continue
		}
		seen[m.Host] = true
		out = append(out, m.Host)
	}
	sort.Strings(out) // a stable starting order, so the shuffle below is the only randomness
	return out
}

// pickHosts takes at most n of them, chosen without favour.
//
// Stale hosts are in the draw with the rest, deliberately. A machine nobody has heard from is the
// one worth reaching, and skipping it is how a cluster would fail to notice a companion coming
// back: it went quiet, so nobody asked, so it stayed quiet until it was forgotten.
func pickHosts(hosts []string, n int, rng *rand.Rand) []string {
	if len(hosts) <= n {
		return hosts
	}
	rng.Shuffle(len(hosts), func(i, j int) { hosts[i], hosts[j] = hosts[j], hosts[i] })
	return hosts[:n]
}

func describeMember(m cluster.Member, now time.Time) string {
	name := m.Name
	if name == "" {
		name = filepath.Base(m.Workdir)
	}
	line := name
	if m.Team != "" {
		line += " [" + m.Team + "]"
	}
	if m.Host != "" {
		line += " (" + m.Host + ")"
	}
	if m.Role != "" {
		line += " — " + m.Role
	}
	if !m.Fresh(now) {
		// Named rather than hidden: a companion nobody has seen for a while is exactly the one
		// somebody is looking for when they run this.
		line += "   [not seen for " + now.Sub(m.Seen).Round(time.Minute).String() + "]"
	}
	return line
}
