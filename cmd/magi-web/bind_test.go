package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The refusal to serve the network is the whole of this console's security, and until now nothing
// checked that it stayed a refusal.
//
// It is one `if` in a startup path no test ran. Deleted or inverted by somebody tidying flags, the
// binary would come up perfectly, serve the fleet page to the whole subnet, and say so only in a
// line on a terminal that scrolled past — the failure mode being that everything WORKS.
func TestTheConsoleRefusesToBindWhereTheNetworkCanReachIt(t *testing.T) {
	// A port nobody else holds, borrowed and given back, so the refusal below can be checked for
	// having released it — the only way to tell a guard that closed the socket from one that
	// merely stopped talking about it.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	wide := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := listenLoopback(wide)
	if err == nil {
		ln.Close()
		t.Fatalf("%s was accepted — this console has no authentication and would be answering the "+
			"network", wide)
	}
	if ln != nil {
		t.Error("a refused address handed back a listener, which would serve anyway")
	}
	// The operator has to be able to act on it. "not loopback" alone leaves somebody who wanted
	// their console from another machine with nowhere to go but turning the check off.
	if !strings.Contains(err.Error(), "ssh -L") {
		t.Errorf("the refusal does not say how to reach it from elsewhere: %v", err)
	}
	// Released, not left bound for the life of the process.
	again, err := net.Listen("tcp", wide)
	if err != nil {
		t.Fatalf("the refused port is still held: %v", err)
	}
	again.Close()

	ok, err := listenLoopback("127.0.0.1:0")
	if err != nil {
		t.Fatalf("loopback was refused: %v", err)
	}
	ok.Close()
}

// One door, so there is one lock.
//
// The check above is worth exactly as much as the guarantee that nothing else in this binary opens
// a port. A second net.Listen — a metrics endpoint, a debug server, a second console for a peer —
// would be a second door, and nothing about it would look wrong in review.
func TestNothingElseInThisBinaryOpensAPort(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // tests bind their own sockets, and none of them serves this console
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "net.Listen(") || strings.Contains(line, "ListenAndServe") {
				found = append(found, f+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "main.go:") {
		t.Errorf("expected exactly one port to be opened, inside listenLoopback in main.go; found:\n%s",
			strings.Join(found, "\n"))
	}
}
