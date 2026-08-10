package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/port"
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

// canFor counts what a workspace advertises being able to do: its written procedures plus the tool
// servers it can reach.
//
// The same two things `about` shows a companion asking what somebody else does, counted rather than
// listed — so the number a hub election turns on is the number a reader would arrive at from the
// advertisement. Two derivations of "how much can it do" would disagree the first time one changed.
func canFor(store *jsonl.Store, plat port.Platform) func(string) int {
	reader := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	return func(workdir string) int {
		if workdir == "" {
			return 0
		}
		n := len(reader.Skills(workdir))
		if c, err := config.Load(filepath.Join(workdir, ".magi")); err == nil {
			n += len(c.MCP)
		}
		return n
	}
}

// exchangeMembers is the `--members` half: read theirs, write ours.
func exchangeMembers(in io.Reader, out, errOut io.Writer, configDir string, can func(string) int) int {
	heard := readMemberList(in, errOut)
	known, err := daemon.LearnMembers(configDir, heard, time.Now(), can)
	if err != nil {
		// Say it, and still answer. The caller asked who is out there; failing to record what they
		// told us is not a reason to leave them knowing nothing.
		fmt.Fprintln(errOut, "magi: could not record what was heard:", err)
		known = daemon.Known(configDir, time.Now(), can)
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
func joinTheCluster(out, errOut io.Writer, configDir, host string, can func(string) int) int {
	mine, err := json.Marshal(daemon.Known(configDir, time.Now(), can))
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	// The remote binary is named as plain `magi`, found on their PATH. Not this machine's path to
	// it: the two are the same program and rarely the same install, and sending an absolute path
	// from here is how a join fails on a host where it happens to live somewhere else.
	cmd := exec.Command("ssh", host, "magi", "--members")
	cmd.Stdin = bytes.NewReader(mine)
	cmd.Stderr = errOut
	said, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(errOut, "magi: %s did not answer: %v\n", host, err)
		fmt.Fprintf(errOut, "      It needs magi on its PATH and this machine needs to be able to "+
			"`ssh %s`.\n", host)
		return 1
	}
	var heard []cluster.Member
	if err := json.Unmarshal(said, &heard); err != nil {
		fmt.Fprintf(errOut, "magi: %s answered with something that is not a member list: %v\n", host, err)
		return 1
	}
	known, err := daemon.LearnMembers(configDir, heard, time.Now(), can)
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
