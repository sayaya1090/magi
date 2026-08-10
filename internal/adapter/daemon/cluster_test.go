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

	mine := Mine(cfg, time.Now())
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

// What a workspace can do is counted once, by the daemon that owns it, and travels on the record.
//
// It used to be handed in by every reader, which put the count in the hands of processes that
// cannot see a remote workspace's skills — so the same companion would be worth seven to itself and
// nothing to anybody else, and a hub election would come out differently depending on who ran it.
func TestTheCapabilityCountTravelsOnTheRecord(t *testing.T) {
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
	unpub, err := Publish(sock, "/w/a", "s_a", Identity{Name: "a", Can: 7})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpub)

	got := Mine(cfg, time.Now())
	if len(got) != 1 || got[0].Can != 7 {
		t.Fatalf("the count the daemon published did not reach the member: %+v", got)
	}
}
