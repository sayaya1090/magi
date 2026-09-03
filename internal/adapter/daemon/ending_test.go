package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listening starts a daemon on a temp socket and returns it with a stop for the test.
func listening(t *testing.T) (*Daemon, context.Context, context.CancelFunc, chan error) {
	t.Helper()
	// A unix socket path has about 100 bytes to live in, and the default temp dir plus a Go
	// subtest name is already past it.
	home, err := os.MkdirTemp(shortRoot(), "mgend")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	d, err := Listen(filepath.Join(home, "d.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Serve(ctx, (*omniEngine)(nil)) }()
	return d, ctx, cancel, done
}

// waitServe waits for Serve to return, or says it did not.
func waitServe(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve ended with %v; these endings are not errors", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve never returned")
	}
}

// A daemon that stops says how it stopped, and the two ways are told apart.
//
// Both are a clean `return nil` — neither is an error and neither should be — so the return value
// cannot carry this, and nothing else did. Measured while chasing a daemon that died three times in
// one session: it left three startup lines and no ending at all, so the last thing its log said was
// that it was serving. Somebody stopping a companion and something killing the process are not the
// same event, and a log that renders them identically sends the reader looking in the wrong place.
func TestADaemonSaysHowItStopped(t *testing.T) {
	t.Run("asked over the socket", func(t *testing.T) {
		d, _, cancel, done := listening(t)
		defer cancel()
		d.Stop()
		waitServe(t, done)
		if got := d.Ending(); got != "asked to stop over the socket" {
			t.Errorf("a shutdown somebody asked for is reported as %q", got)
		}
	})

	t.Run("interrupted", func(t *testing.T) {
		d, _, cancel, done := listening(t)
		cancel()
		waitServe(t, done)
		// The exact words are not the contract; not confusing it with a request is.
		if got := d.Ending(); got == "asked to stop over the socket" {
			t.Errorf("a killed daemon reports itself as having been asked to stop (%q) — the log "+
				"then sends the reader looking for whoever asked, and nobody did", got)
		}
	})

	t.Run("restarting", func(t *testing.T) {
		d, _, cancel, done := listening(t)
		defer cancel()
		d.Restart()
		waitServe(t, done)
		if got := d.Ending(); got != "restarting onto the binary on disk" {
			t.Errorf("a self-update restart is reported as %q, which reads as a companion that "+
				"went away rather than one that is coming back", got)
		}
	})
}
