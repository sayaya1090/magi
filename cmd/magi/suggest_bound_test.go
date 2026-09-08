package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// A composer suggestion must come back even when the daemon never answers.
//
// The attached view's other calls ride a pooled connection that is deliberately unbounded — an
// attached view is meant to sit on its daemon — and SuggestPrompt used the same unbounded dial
// while its own comment claimed it was "bounded at seconds". Nothing bounded it: the ctx it is
// handed is context.Background (the TUI is built from one) and the client sets a deadline only
// when the dial gave it one. So a daemon that accepted and then went quiet held this for ever,
// and the composer re-fires on every debounce — a connection per keystroke burst, none returning.
//
// The fixture is a daemon that ACCEPTS AND SAYS NOTHING, which is the state this exists for: a
// socket whose owner is wedged rather than gone. A dial to a socket nobody accepts fails at once
// and would prove nothing.
//
// This test costs about completeDeadline in wall clock, on purpose. The bound is the behaviour,
// so waiting for it is the measurement; asserting on the constant instead would pass with the
// unbounded dial still in place.
func TestASuggestionComesBackFromASilentDaemon(t *testing.T) {
	dir := shortSockDir(t)
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			// Held open and never written to. Closing here would let the read fail immediately,
			// which is the case that already worked.
			t.Cleanup(func() { c.Close() })
		}
	}()

	a := attached{sock: sock}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, serr := a.SuggestPrompt(context.Background(), "s_x", "fix the ")
		done <- serr
	}()
	select {
	case serr := <-done:
		if serr == nil {
			t.Fatal("a daemon that never answered produced a suggestion")
		}
		if took := time.Since(start); took < completeDeadline/2 {
			t.Fatalf("returned in %s — too fast to have been the deadline; the fixture is "+
				"probably refusing the connection rather than accepting and going quiet", took)
		}
	case <-time.After(completeDeadline + 20*time.Second):
		t.Fatalf("SuggestPrompt did not come back within %s of its own bound — it is unbounded",
			completeDeadline)
	}
}
