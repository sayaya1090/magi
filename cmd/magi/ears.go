package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/config"
)

// Keeping this magi's peer attachments in step with the companions actually running.
//
// # What changed, and why it was refused before
//
// A companion is two tools in this magi's list, spawned as a subprocess of this binary. They used
// to be attached once, at startup, and the comment saying so gave a reason for not watching: a
// manager that added and removed tools underneath a running turn would change the tool list
// between one step and the next, and a model calling something that has just gone is a failure
// nobody can read afterwards. That reason is right, and it rules out a clock.
//
// It does not rule out a boundary. Between turns nothing is stepping, so the set can change
// without anything being in a position to notice mid-thought — see app.Config.BetweenTurns. That
// is the only moment this runs.
//
// # Both directions, and the second one is the point
//
// A companion that came up later gets attached, which is what makes it answerable as well as
// addressable: the roster already advertises it, so without this a model could hand it work and
// then have no way to ask it anything. A companion that STOPPED gets detached, which matters more
// — a tool in the list that can only fail is worse than one that is not there, and it is the shape
// this tree keeps writing down.
//
// # A listing that failed changes nothing
//
// It is this machine having a bad moment, not the cluster emptying. Detaching every peer on it
// would take away tools that work, which is a worse answer than a stale set.
// peers is the part of the MCP manager this needs: attach one companion, detach one.
//
// An interface and not the manager, because attaching spawns a subprocess that has to speak the
// protocol before it counts as attached — so a test of "which companions should be attached right
// now" would otherwise have to start real ones to ask a question that is about a set difference.
type peers interface {
	AddStdio(ctx context.Context, name, command string, args, env []string) error
	Remove(name string)
}

type companionEars struct {
	mgr peers
	// list is who is running here. Injected rather than assembled from a store and a directory,
	// because that is the one thing this can be wrong about and the one thing a caller cannot
	// otherwise make fail on purpose — including the failure the paragraph above is about.
	list func(context.Context) ([]fleet.Agent, error)
	cfg  config.Config
	// self is this binary and me is what peers see on anything said through their ear. Fixed for
	// the process.
	self, me string
	errOut   io.Writer

	mu       sync.Mutex
	attached map[string]bool
	// said keeps a complaint from being repeated every turn. A name is forgotten when it is
	// DETACHED, which is the only way back to being attempted again — so a companion that stops,
	// comes back and fails to start is reported again rather than silently missing.
	said map[string]bool
}

func newCompanionEars(mgr peers, list func(context.Context) ([]fleet.Agent, error),
	cfg config.Config, self, me string, errOut io.Writer) *companionEars {

	return &companionEars{mgr: mgr, list: list, cfg: cfg, self: self, me: me, errOut: errOut,
		attached: map[string]bool{}, said: map[string]bool{}}
}

// reconcile attaches the companions running here and detaches the ones that are not.
//
// Nil-safe on the receiver: it is wired into the engine's config before the MCP manager exists,
// which is the same order everything else in this file is built in.
func (e *companionEars) reconcile(ctx context.Context) {
	if e == nil || e.mgr == nil || e.list == nil || !e.cfg.Companion.MCPPeers {
		return
	}
	list, err := e.list(ctx)
	if err != nil {
		return // a bad moment here is not the cluster emptying; keep what works
	}
	peers, clashed := companionPeers(list, e.cfg)

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, name := range clashed {
		e.complain(name, fmt.Sprintf("magi: mcp peers: not attaching the companion %q — an "+
			"[mcp.%s] in this workspace's config already has that name. Rename one of them.",
			name, name))
	}
	want := make(map[string]bool, len(peers))
	for _, a := range peers {
		want[a.Name] = true
	}
	for name := range e.attached {
		if want[name] {
			continue
		}
		// Gone. Removing takes its tools out of the list and closes the subprocess, so the next
		// turn is not offered a door onto a companion that stopped.
		e.mgr.Remove(name)
		delete(e.attached, name)
		delete(e.said, name)
		fmt.Fprintf(e.errOut, "magi: mcp peer %q: no longer running here, detached\n", name)
	}
	for _, a := range peers {
		if e.attached[a.Name] {
			continue
		}
		// Named after the companion, so the tools arrive as design__knows rather than as a number.
		// A model with four of these attached decides which to ask by that name.
		if aerr := e.mgr.AddStdio(ctx, a.Name, e.self,
			[]string{"--mcp", a.Name, "--mcp-as", e.me}, nil); aerr != nil {
			// One peer that will not start is not a reason to withhold the others, and it is worth
			// saying: a silent failure here looks like a companion that knows nothing. Not marked
			// attached, so the next turn tries again.
			e.complain(a.Name, fmt.Sprintf("magi: mcp peer %q: %v", a.Name, aerr))
			continue
		}
		e.attached[a.Name] = true
	}
}

// complain says something once per name until that name works. Called with mu held.
func (e *companionEars) complain(name, line string) {
	if e.said[name] {
		return
	}
	e.said[name] = true
	fmt.Fprintln(e.errOut, line)
}

// selfBinary is this executable, which is what a peer attachment runs with `--mcp <name>`.
func selfBinary() string {
	self, err := os.Executable()
	if err != nil {
		// Reported once, here, rather than per reconcile: without it nothing can be attached at
		// all, and an empty peer set with no explanation reads as a companion that knows nobody.
		fmt.Fprintln(os.Stderr, "magi: mcp peers: cannot find this binary:", err)
		return ""
	}
	return self
}

// meCalled is what peers see on anything this companion says through their ear. Taken from the
// declared name, falling back to the workspace, because "a message from somebody" with no way to
// tell which somebody is worse than a long path.
func meCalled(cfg config.Config, wd string) string {
	if n := strings.TrimSpace(cfg.Companion.Name); n != "" {
		return n
	}
	return filepath.Base(wd)
}
