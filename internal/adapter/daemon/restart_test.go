package daemon

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// A restart request drains the daemon exactly like a shutdown — Serve returns, the socket and claim
// are released — but leaves Restarting() true, which is how the daemon loop knows to re-exec onto the
// new binary instead of exiting. The re-exec itself is not exercised here (it would replace the test
// process); this pins the protocol + drain + flag that drive it.
func TestRestartDrainsTheDaemonAndFlagsARelaunch(t *testing.T) {
	sock := filepath.Join(shortDir(t), "daemon-r.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Serve(context.Background(), &fakeEngine{}) }()

	// Wait until it is listening.
	var cl *Client
	for i := 0; i < 100; i++ {
		if c, derr := Dial(sock); derr == nil {
			cl = c
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cl == nil {
		t.Fatal("daemon never came up")
	}
	defer cl.Close()

	if err := cl.Restart(); err != nil {
		t.Fatalf("restart request refused: %v", err)
	}
	select {
	case serr := <-done:
		if serr != nil {
			t.Errorf("Serve ended with an error rather than a clean drain: %v", serr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a restart request did not drain the daemon")
	}
	if !d.Restarting() {
		t.Error("Restarting() is false after a restart request — the daemon loop would exit instead of re-exec")
	}
	// The socket is released, so a fresh Listen (what the successor does) can claim it.
	if _, derr := net.Dial("unix", sock); derr == nil {
		t.Error("the socket is still answering after the drain — the successor could not rebind")
	}
}

// A plain shutdown must NOT flag a relaunch: the two share a drain but differ in the ending.
func TestShutdownDoesNotFlagARelaunch(t *testing.T) {
	sock := filepath.Join(shortDir(t), "daemon-r.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Serve(context.Background(), &fakeEngine{}) }()
	var cl *Client
	for i := 0; i < 100; i++ {
		if c, derr := Dial(sock); derr == nil {
			cl = c
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cl == nil {
		t.Fatal("daemon never came up")
	}
	defer cl.Close()
	if err := cl.Shutdown(); err != nil {
		t.Fatalf("shutdown refused: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a shutdown request did not drain the daemon")
	}
	if d.Restarting() {
		t.Error("a shutdown flagged a relaunch — the process would re-exec when it was asked to stop")
	}
	// And a Restart AFTER the stop began must not resurrect it: the auto-update loop can poll its way
	// here during a drain, and the flag flipping late would re-exec a daemon the operator stopped.
	d.Restart()
	if d.Restarting() {
		t.Error("Restart() after a shutdown set the relaunch flag — stop must win")
	}
}
