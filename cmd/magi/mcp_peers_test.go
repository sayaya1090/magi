package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/config"
)

// A companion never attaches itself.
//
// Obvious, and worth a test precisely because it is: the rule was one clause in a loop that also
// spawned subprocesses, so nothing could check it. What it prevents is not merely a wasted
// subprocess. `magi --mcp <name>` resolves that name over the SAME roster this list came from, so a
// magi that reached itself would answer itself — and what comes back is what retrieval had already
// put in front of it, one round trip and one held-open process later.
func TestACompanionDoesNotAttachItself(t *testing.T) {
	list := []fleet.Agent{
		{Name: "design", Live: true},
		{Name: "me", Live: true, Here: true},
		{Name: "api", Live: true},
	}
	peers, _ := companionPeers(list, config.Config{})
	for _, p := range peers {
		if p.Here {
			t.Fatalf("this companion attached itself: %+v", p)
		}
	}
	if got := names(peers); got != "api design" && got != "design api" {
		t.Errorf("attached %q, want design and api", got)
	}
}

// A dead companion is a socket file with nobody behind it, and a nameless one has nothing to
// namespace its tools by or for the child to resolve.
func TestOnlyLiveNamedCompanionsAreAttached(t *testing.T) {
	list := []fleet.Agent{
		{Name: "design", Live: true},
		{Name: "gone", Live: false},
		{Name: "", Live: true},
	}
	peers, _ := companionPeers(list, config.Config{})
	if got := names(peers); got != "design" {
		t.Errorf("attached %q, want design alone", got)
	}
}

// A companion whose name the operator already used for an [mcp] server is NOT attached, and is
// named out loud.
//
// The manager refuses a second server under one name — before that it took the second silently,
// leaving the first connection out of its map, never closed and its tools still registered. Here
// the config wins, because it is the one a person typed on purpose. But the companion is reported
// rather than dropped: missing from the tool list, it is indistinguishable from one not running,
// and the operator would go looking for a daemon that is up.
func TestACompanionDoesNotStealANameTheConfigUses(t *testing.T) {
	list := []fleet.Agent{{Name: "design", Live: true}, {Name: "api", Live: true}}
	cfg := config.Config{MCP: map[string]config.MCPServer{"design": {Command: "figma-mcp"}}}

	peers, clashed := companionPeers(list, cfg)
	if got := names(peers); got != "api" {
		t.Errorf("attached %q, want api alone", got)
	}
	if len(clashed) != 1 || clashed[0] != "design" {
		t.Errorf("the clash was not reported: %v", clashed)
	}
}

func names(as []fleet.Agent) string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return strings.Join(out, " ")
}

// A companion on another machine is NOT attached as a peer here.
//
// The attach spawns THIS machine's binary with `--mcp <name>`, which resolves the name against
// companions published HERE. For a companion on buildbox that is either a peer that fails to start
// or — on two machines set up by one person, where the same names and the same checkout paths
// recur — a door onto an entirely different companion, opened under the remote one's name.
//
// Live is what this filter reads, and a remote row is Live when somebody sighted it recently: true,
// and not the fact this filter needs. The state beside it is.
func TestACompanionOnAnotherMachineIsNotAttachedAsALocalPeer(t *testing.T) {
	list := []fleet.Agent{
		{Name: "here", Socket: "/s/a.sock", Live: true, State: fleet.Idle},
		{Name: "design", Socket: "/far/d.sock", Host: "buildbox", Live: true,
			Elsewhere: true, State: fleet.Idle},
	}
	peers, _ := companionPeers(list, config.Config{})
	for _, p := range peers {
		if p.Name == "design" {
			t.Fatal("a companion on buildbox would be started as a local MCP peer, under its name")
		}
	}
	if len(peers) != 1 {
		t.Fatalf("%d peers, want just the local one: %+v", len(peers), peers)
	}
}
