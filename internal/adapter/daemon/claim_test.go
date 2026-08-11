package daemon

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two daemons must never end up on one socket path.
//
// The three steps that guard it — ask who is listening, remove the leftover, bind — have two gaps,
// and simultaneous starts fall through both: each finds nothing, each removes what the other just
// bound, and one engine is left orphaned while both keep writing the same session log. Which is the
// one thing the store cannot survive: two writers numbering events from two counters.
//
// Measured at 25 of 300 simultaneous starts before the claim existed. Not a corner — a coin flip
// with a bias.
func TestOnlyOneDaemonEverOwnsASocket(t *testing.T) {
	dir := shortDir(t)
	const rounds = 60
	for round := 0; round < rounds; round++ {
		sock := filepath.Join(dir, fmt.Sprintf("r%d.sock", round))
		// Listen rather than Serve, and no sleep anywhere. The invariant is "exactly one of them
		// binds", which Listen answers by returning — waiting a fixed 15ms for the loser to be
		// scheduled instead measured how busy the machine was, and reported a pass as a failure
		// once in roughly twenty runs of the suite (observed 2026-08-07 under a full gate).
		var wg sync.WaitGroup
		var mu sync.Mutex
		var held []*Daemon
		var refusals []string
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				d, err := Listen(sock)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					refusals = append(refusals, err.Error())
					return
				}
				// HELD, not closed here. Closing inside the goroutine releases the claim, and two
				// starts that happen to run one after the other would then both bind legitimately
				// — which is correct behaviour reported as the bug. The question is whether two can
				// hold it AT ONCE, so both keep what they got until both have tried.
				held = append(held, d)
			}()
		}
		wg.Wait()
		won := len(held)
		defer func() {
			for _, d := range held {
				d.Close()
			}
		}()

		if won != 1 {
			t.Fatalf("round %d: %d of 2 simultaneous starts bound %s — anything but one means two "+
				"engines writing one log, or none running at all. Refusals: %v",
				round, won, sock, refusals)
		}
		// The loser is told which situation it is in. "another magi is starting or running" is a
		// different thing to read at 3am from a bind error about a file.
		if len(refusals) != 1 || !strings.Contains(refusals[0], "another magi") {
			t.Fatalf("round %d: the refused start said %v", round, refusals)
		}
	}
}

// A live listener that holds no claim is still refused.
//
// That is a daemon from a version before the claim existed, and during an upgrade both are on the
// machine. The dial is what catches it — the claim is free, the socket answers — and the two
// refusals say different things because they know different things: one that another process holds
// the workspace, one that something is listening on the path.
func TestAListenerWithNoClaimIsStillRefused(t *testing.T) {
	sock := filepath.Join(shortDir(t), "old.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			c.Close()
		}
	}()

	err = Serve(context.Background(), &fakeEngine{}, sock)
	if err == nil {
		t.Fatal("a daemon started on top of a live listener")
	}
	if !contains(err.Error(), "already listening") {
		t.Errorf("the refusal does not name what it found: %v", err)
	}
	// And the socket it found is still there: refusing must not remove somebody else's.
	if _, serr := os.Stat(sock); serr != nil {
		t.Errorf("the refusing daemon removed the live socket: %v", serr)
	}
}

// A daemon that died without cleaning up must not lock the workspace out forever. This is the case
// the stale-socket removal exists for, and the claim has to keep it working: the kernel drops a
// flock when its holder goes, however it goes.
func TestAWorkspaceIsReusableAfterACrash(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "crashed.sock")

	// What SIGKILL leaves: a socket file, a lock file, and no process.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(sock+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close() // the holder is gone, so the kernel has already dropped its lock
	ln.Close()   // ...but the socket file stays behind, as it does after a kill
	if _, serr := os.Stat(sock); serr != nil {
		// Go's listener removes the socket on Close, so recreate the leftover by hand.
		if f, cerr := os.Create(sock); cerr == nil {
			f.Close()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, &fakeEngine{}, sock) }()
	waitForSocket(t, sock)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("starting in a workspace whose daemon was killed failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("the daemon did not stop")
	}
}

// And after a clean stop the next one starts, which is the ordinary restart.
func TestTheNextDaemonStartsAfterACleanStop(t *testing.T) {
	sock := filepath.Join(shortDir(t), "d.sock")
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Serve(ctx, &fakeEngine{}, sock) }()
		waitForSocket(t, sock)
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("restart %d: %v", i, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("restart %d: the daemon did not stop", i)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// A daemon that loses the race must leave the winner's world exactly as it found it.
//
// This is the shape the live check caught: four `magi --daemon` at once left one daemon serving and
// NO record at all, so `--agents`, the dashboard and `--attach` all reported an empty workspace
// while a turn was running in it. Each loser had published its own session id over the winner's and
// then removed the file on the way out — because publishing happened before the socket was claimed.
//
// Claiming first is what makes the sequence safe, and this pins the property the ordering buys:
// nothing a loser does reaches the winner's socket or record.
func TestALoserLeavesTheWinnersRecordAlone(t *testing.T) {
	sock := filepath.Join(shortDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	winner, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = winner.Serve(ctx, &fakeEngine{}) }()
	waitForSocket(t, sock)
	unpublish, err := Publish(sock, "/w/winner", "s_winner", Identity{})
	if err != nil {
		t.Fatal(err)
	}
	defer unpublish()

	for i := 0; i < 3; i++ {
		if _, lerr := Listen(sock); lerr == nil {
			t.Fatal("a second daemon bound a socket that was already claimed")
		}
	}

	// The record still names the daemon that is actually running.
	in, err := Published(sock)
	if err != nil {
		t.Fatalf("the record is gone after three refused starts: %v", err)
	}
	if in.Session != "s_winner" || in.Workdir != "/w/winner" {
		t.Errorf("the record was overwritten by a process that never served: %+v", in)
	}
	// And the socket still answers.
	cl, err := Dial(sock)
	if err != nil {
		t.Fatalf("the winner's socket stopped answering: %v", err)
	}
	cl.Close()
}

// Binding and then giving up releases everything, so the next start is not blocked by a process
// that never served.
func TestGivingUpAfterBindingReleasesTheClaim(t *testing.T) {
	sock := filepath.Join(shortDir(t), "d.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("closing a bound-but-unserved daemon: %v", err)
	}
	if _, serr := os.Stat(sock); !os.IsNotExist(serr) {
		t.Error("the socket outlived the daemon that never served on it")
	}
	d2, err := Listen(sock)
	if err != nil {
		t.Fatalf("the next start was blocked by a claim nobody holds: %v", err)
	}
	d2.Close()
}

// Ctrl-C stops a daemon that somebody is looking at.
//
// It did not. Cancelling the context closed the listener and then waited for every connection
// goroutine, each of which sits in a blocking read until its peer hangs up — and a console holds
// one open per daemon it has ever touched, while an attached terminal holds one for as long as it
// is attached. So a daemon anybody had opened printed nothing on Ctrl-C and never exited; the only
// way out was to kill it. Reported from Windows, and nothing about it was Windows.
func TestCtrlCStopsADaemonSomebodyIsConnectedTo(t *testing.T) {
	sock := filepath.Join(shortDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, &fakeEngine{}, sock) }()
	waitForSocket(t, sock)

	// A viewer: connected, idle, and holding the connection the way every console does.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// One request, so the connection is genuinely being served rather than merely accepted.
	if _, err := conn.Write([]byte(`{"method":"status"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("stopping with a viewer attached failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the daemon did not stop while a viewer held a connection — this is the hang, and " +
			"the only way out of it is to kill the process")
	}
}
