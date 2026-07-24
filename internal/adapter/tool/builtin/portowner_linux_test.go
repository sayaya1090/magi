//go:build linux

package builtin

import (
	"net"
	"os"
	"testing"
)

// On Linux, findPortOwners must locate THIS process as the owner of a port we bind,
// via the real /proc scan — the end-to-end path the tool depends on in bench containers.
func TestFindPortOwnersFindsSelf(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	owners, supported := findPortOwners(port)
	if !supported {
		t.Fatal("Linux must report supported=true")
	}
	self := os.Getpid()
	var found bool
	for _, o := range owners {
		if o.pid == self {
			found = true
			if o.state != "LISTEN" {
				t.Errorf("own listener should be LISTEN, got %q", o.state)
			}
		}
	}
	if !found {
		t.Fatalf("findPortOwners(%d) did not include our own pid %d; got %+v", port, self, owners)
	}
}

// A port nobody is bound to yields no owners (supported, empty) — the "port is free" signal.
func TestFindPortOwnersEmptyWhenFree(t *testing.T) {
	// Bind then immediately close to obtain a port very likely still free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	owners, supported := findPortOwners(port)
	if !supported {
		t.Fatal("Linux must report supported=true")
	}
	// May briefly linger in TIME_WAIT (inode 0, filtered), so we only assert no LIVE owner.
	for _, o := range owners {
		t.Errorf("freed port unexpectedly has owner pid %d (%s)", o.pid, o.state)
	}
}
