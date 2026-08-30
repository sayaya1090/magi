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

	// And one that is what a kill leaves behind: a socket with nobody listening.
	deadSock := filepath.Join(cfg, "daemon-dead.sock")
	leaveStaleSocket(t, deadSock)
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

// A record says this machine's name the one way the rest of the system says it.
//
// Stated directly rather than left to a test that happens to run on a machine with a capital in its
// hostname — which is how the second spelling survived: everything agreed on the developer's laptop
// and an election elsewhere decided the companion it had just elected was on no row it could see.
func TestAPublishedRecordSaysThisMachineTheSameWayEverythingElseDoes(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-h.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unpub, err := Publish(sock, "/w/a", "s_a", Identity{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpub)
	in, err := Published(sock)
	if err != nil {
		t.Fatal(err)
	}
	if in.Host != Host() {
		t.Fatalf("the record says %q and everything else says %q", in.Host, Host())
	}
}

// What a companion has waiting is published as it changes, and travels with the sighting.
//
// Everything else on a record is fixed when the daemon starts. This one moves on the scale of
// seconds, which is why it has a writer of its own — and why a reader on another machine gets it
// as old as the sighting, with the refusal as the authority.
func TestWhatIsWaitingIsPublishedAsItChanges(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-w.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	unpub, err := Publish(sock, "/w/a", "s_a", Identity{Name: "design", Can: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpub)

	if err := Announce(sock, 3, true); err != nil {
		t.Fatal(err)
	}
	in, err := Published(sock)
	if err != nil {
		t.Fatal(err)
	}
	if in.Waiting != 3 || !in.Handling {
		t.Fatalf("the record says %d waiting, handling=%v", in.Waiting, in.Handling)
	}
	// And nothing else on the record was lost rewriting it.
	if in.Name != "design" || in.Can != 2 || in.Session != "s_a" {
		t.Errorf("the rest of the record did not survive: %+v", in)
	}
	// It reaches a member, which is how it crosses a machine.
	got := Mine(cfg, time.Now())
	if len(got) != 1 || got[0].Waiting != 3 || !got[0].Handling {
		t.Fatalf("the sighting does not carry it: %+v", got)
	}
	// A daemon on its way out is not resurrected by a number about nothing.
	unpub()
	if err := Announce(sock, 9, false); err != nil {
		t.Fatalf("announcing to a record that is gone errored: %v", err)
	}
	if _, err := Published(sock); err == nil {
		t.Error("the record came back")
	}
}
