package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A companion this machine knows is dead is never passed on.
//
// The worst thing gossip can do is spread something the spreader already knew was false. A socket
// file whose daemon was SIGKILLed is exactly that: this process can see there is nobody behind it,
// and putting it into other machines' lists gives them a companion that takes an hour to expire and
// that nobody further away is in a position to correct.
func TestADeadCompanionIsNotGossiped(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magiclu")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })

	// One with somebody behind it.
	liveSock := filepath.Join(cfg, "daemon-live.sock")
	ln, err := net.Listen("unix", liveSock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	unpubLive, err := Publish(liveSock, "/w/live", "s_live", Identity{Name: "alive"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpubLive)

	// And one that is what a kill leaves behind: a file with nobody listening.
	deadSock := filepath.Join(cfg, "daemon-dead.sock")
	if err := os.WriteFile(deadSock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unpubDead, err := Publish(deadSock, "/w/dead", "s_dead", Identity{Name: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpubDead)

	mine := Mine(cfg, time.Now(), nil)
	for _, m := range mine {
		if m.Name == "ghost" {
			t.Errorf("a companion with nobody behind its socket was offered to the cluster: %+v", m)
		}
	}
	found := false
	for _, m := range mine {
		if m.Name == "alive" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the live companion is missing: %+v", mine)
	}
}

// What a workspace can do is counted by whoever wired this up, and a caller with no way to work it
// out passes nothing rather than a wrong number.
func TestTheCapabilityCountComesFromTheCaller(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "magiclu2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	sock := filepath.Join(cfg, "daemon-a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	unpub, err := Publish(sock, "/w/a", "s_a", Identity{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpub)

	got := Mine(cfg, time.Now(), func(workdir string) int {
		if workdir != "/w/a" {
			t.Errorf("counted for %q", workdir)
		}
		return 7
	})
	if len(got) != 1 || got[0].Can != 7 {
		t.Fatalf("the count did not reach the member: %+v", got)
	}
	// nil is a caller that cannot count, and zero is the honest answer — an election falls back to
	// its stable ordering rather than acting on a number nobody supplied.
	if got := Mine(cfg, time.Now(), nil); len(got) != 1 || got[0].Can != 0 {
		t.Errorf("a nil counter produced %+v", got)
	}
}
