package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// updaterEngine is an Engine (via the embedded fake) that also updates itself with a canned result —
// stands in for the real daemon self-update so the `update` method's wiring can be driven without a
// download.
type updaterEngine struct {
	*fakeEngine
	res UpdateResult
	err error
}

func (u updaterEngine) Update(context.Context) (UpdateResult, error) { return u.res, u.err }

func serveUpdater(t *testing.T, eng Engine) (*Daemon, *Client, func()) {
	t.Helper()
	sock := filepath.Join(shortDir(t), "daemon-u.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Serve(ctx, eng) }()
	var cl *Client
	for i := 0; i < 100; i++ {
		if c, derr := Dial(sock); derr == nil {
			cl = c
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cl == nil {
		cancel()
		t.Fatal("daemon never came up")
	}
	return d, cl, func() { cl.Close(); cancel() }
}

// A successful update reports what it did and then restarts onto the new binary — the whole point of
// B1: it wires the self-update (B5) to the relaunch (B2).
func TestUpdateRunsTheUpdaterAndRestartsOnSuccess(t *testing.T) {
	eng := updaterEngine{fakeEngine: &fakeEngine{}, res: UpdateResult{Updated: true, From: "v0.22.2", To: "v0.23.0"}}
	sock := filepath.Join(shortDir(t), "daemon-u.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Serve(context.Background(), eng) }()
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

	out, err := cl.Update()
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "v0.22.2") || !strings.Contains(out, "v0.23.0") {
		t.Errorf("the reply does not say what it updated: %q", out)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a successful update did not restart the daemon")
	}
	if !d.Restarting() {
		t.Error("a successful update did not flag a relaunch")
	}
}

// An update that finds nothing new says so and does NOT restart — a daemon does not bounce to install
// the version it already runs.
func TestUpdateThatIsAlreadyCurrentDoesNotRestart(t *testing.T) {
	eng := updaterEngine{fakeEngine: &fakeEngine{}, res: UpdateResult{Updated: false, Message: "already up to date"}}
	d, cl, stop := serveUpdater(t, eng)
	defer stop()

	out, err := cl.Update()
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("the reply does not say it was current: %q", out)
	}
	// Give any erroneous restart a moment, then confirm it did not happen.
	time.Sleep(100 * time.Millisecond)
	if d.Restarting() {
		t.Error("an already-current update flagged a relaunch")
	}
}

// A failed update surfaces the error and does not restart — a rollback (the binary on disk is the old
// one) must not be followed by a relaunch that would come up on nothing new.
func TestUpdateThatFailsDoesNotRestart(t *testing.T) {
	eng := updaterEngine{fakeEngine: &fakeEngine{}, err: context.DeadlineExceeded}
	d, cl, stop := serveUpdater(t, eng)
	defer stop()

	if _, err := cl.Update(); err == nil {
		t.Error("a failed update reported success")
	}
	time.Sleep(100 * time.Millisecond)
	if d.Restarting() {
		t.Error("a failed update flagged a relaunch")
	}
}
