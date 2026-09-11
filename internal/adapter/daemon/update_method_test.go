package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
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

// busyEngine is an updater that is in the middle of something until told otherwise.
type busyEngine struct {
	updaterEngine
	working chan struct{} // closed when the work is over
}

func (b busyEngine) Busy() bool {
	select {
	case <-b.working:
		return false
	default:
		return true
	}
}

// Asking for "idle" while a turn runs must not throw that turn away: the binary is committed now and
// the restart waits. The reply still goes out at once — a person pressing a button and being left
// without an answer reads as a hang.
func TestUpdateAskedToWaitRestartsOnlyWhenNothingIsRunning(t *testing.T) {
	was := idlePoll
	idlePoll = 5 * time.Millisecond
	defer func() { idlePoll = was }()

	working := make(chan struct{})
	eng := busyEngine{
		updaterEngine: updaterEngine{fakeEngine: &fakeEngine{}, res: UpdateResult{Updated: true, From: "v1", To: "v2"}},
		working:       working,
	}
	sock := filepath.Join(shortDir(t), "daemon-u.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	// Restart drains the daemon exactly as Stop does, so Serve returning IS the restart — and
	// Restarting() below says which of the two ended it.
	restarted := make(chan error, 1)
	go func() { restarted <- d.Serve(context.Background(), eng) }()
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

	out, uerr := cl.UpdateWhen("idle")
	if uerr != nil {
		t.Fatal(uerr)
	}
	// The answer arrives while the work is still running, and says which of the two it did.
	if !strings.Contains(out, "when nothing is running") {
		t.Errorf("the reply does not say the restart was deferred: %q", out)
	}
	select {
	case <-restarted:
		t.Fatal("it restarted while a turn was in flight — that is the turn thrown away")
	case <-time.After(50 * time.Millisecond):
	}

	close(working) // the turn ends
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("it never restarted after the work finished — the update is on disk and nobody is on it")
	}
	if !d.Restarting() {
		t.Error("it ended, but as a stop rather than a relaunch — the new build would wait for a hand")
	}
}

// And the deliberate "now": the same door, the same busy daemon, no waiting. This is what a client
// offers as "end what is running", and what every older client sends by saying nothing.
func TestUpdateWithoutWaitingRestartsEvenWhileBusy(t *testing.T) {
	working := make(chan struct{}) // never closed: it stays busy
	eng := busyEngine{
		updaterEngine: updaterEngine{fakeEngine: &fakeEngine{}, res: UpdateResult{Updated: true, From: "v1", To: "v2"}},
		working:       working,
	}
	sock := filepath.Join(shortDir(t), "daemon-u.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	// Restart drains the daemon exactly as Stop does, so Serve returning IS the restart — and
	// Restarting() below says which of the two ended it.
	restarted := make(chan error, 1)
	go func() { restarted <- d.Serve(context.Background(), eng) }()
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

	out, uerr := cl.Update() // no `when` — the old spelling
	if uerr != nil {
		t.Fatal(uerr)
	}
	if strings.Contains(out, "when nothing is running") {
		t.Errorf("a caller that asked for nothing got the waiting behaviour: %q", out)
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("it did not restart — an older client's update would never take effect")
	}
	if !d.Restarting() {
		t.Error("it ended, but as a stop rather than a relaunch")
	}
}

// quiescingEngine can shut the door, and records whether it was asked to.
type quiescingEngine struct {
	updaterEngine
	mu      sync.Mutex
	working bool
	held    bool
	holds   int
}

func (q *quiescingEngine) HoldForUpdate() (func(), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.working || q.held {
		return nil, false
	}
	q.held, q.holds = true, q.holds+1
	return func() { q.mu.Lock(); q.held = false; q.mu.Unlock() }, true
}

// start reports whether a turn could begin — which is the question the hold exists to answer.
func (q *quiescingEngine) start() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.held {
		return false
	}
	q.working = true
	return true
}

func (q *quiescingEngine) stop() { q.mu.Lock(); q.working = false; q.mu.Unlock() }

// The gap Busy leaves: a turn arriving between "nothing is running" and the restart is thrown away by
// a decision that had just concluded there was none. An engine that can hold closes the door in the
// same step it finds it quiet (CLIENT_LIFECYCLE §9.3).
func TestADeferredUpdateShutsTheDoorInTheStepThatFindsItQuiet(t *testing.T) {
	was := idlePoll
	idlePoll = 5 * time.Millisecond
	defer func() { idlePoll = was }()

	eng := &quiescingEngine{
		updaterEngine: updaterEngine{fakeEngine: &fakeEngine{}, res: UpdateResult{Updated: true, From: "v1", To: "v2"}},
		working:       true, // a turn is in flight when the button is pressed
	}
	sock := filepath.Join(shortDir(t), "daemon-u.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	restarted := make(chan error, 1)
	go func() { restarted <- d.Serve(context.Background(), eng) }()
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

	if _, uerr := cl.UpdateWhen("idle"); uerr != nil {
		t.Fatal(uerr)
	}
	select {
	case <-restarted:
		t.Fatal("it restarted while a turn was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	eng.stop() // the turn ends; the next poll may take the hold
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("it never restarted after the work finished")
	}
	if eng.holds == 0 {
		t.Fatal("it restarted without ever taking the hold — the door was open the whole time")
	}
	// ⚠ **And it let go.** Restart refuses when a stop is already draining, so the restart is not
	// guaranteed to end this process — a hold left on after it would be a daemon that keeps serving
	// and silently answers nothing.
	eng.mu.Lock()
	stillHeld := eng.held
	eng.mu.Unlock()
	if stillHeld {
		t.Error("the door was left shut after the restart — a daemon that survives it accepts no work")
	}
	// And while it was held, nothing could have started. Asked of the same lock a run would take.
	if !d.Restarting() {
		t.Error("it ended as a stop rather than a relaunch")
	}
}

// The hold really does refuse a turn — the property the whole thing rests on.
func TestAHeldEngineRefusesToStartWork(t *testing.T) {
	eng := &quiescingEngine{updaterEngine: updaterEngine{fakeEngine: &fakeEngine{}}}
	release, ok := eng.HoldForUpdate()
	if !ok {
		t.Fatal("could not take the hold on an idle engine")
	}
	if eng.start() {
		t.Fatal("a turn started while the door was held shut")
	}
	release()
	if !eng.start() {
		t.Error("the door never reopened")
	}
}
