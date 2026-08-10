package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

type fakeEngine struct {
	mu      sync.Mutex
	got     []string
	fail    error
	waiting *app.Ask // what this engine claims to be blocked on, if anything
}

func (f *fakeEngine) Waiting(session.SessionID) (app.Ask, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waiting == nil {
		return app.Ask{}, false
	}
	return *f.waiting, true
}

func (f *fakeEngine) note(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, s)
	return f.fail
}
func (f *fakeEngine) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func (f *fakeEngine) Submit(_ context.Context, c command.SubmitPrompt) error {
	return f.note("submit:" + string(c.SessionID) + ":" + textOf(c.Parts))
}
func (f *fakeEngine) Steer(_ context.Context, c command.SubmitPrompt) error {
	return f.note("steer:" + string(c.SessionID) + ":" + textOf(c.Parts))
}
func (f *fakeEngine) Interrupt(_ context.Context, c command.Interrupt) error {
	return f.note("interrupt:" + string(c.SessionID))
}
func (f *fakeEngine) RespondPermission(_ context.Context, c command.RespondPermission) error {
	return f.note("permission:" + c.CallID + ":" + c.Decision)
}
func (f *fakeEngine) RespondQuestion(_ context.Context, c command.RespondQuestion) error {
	return f.note("answer:" + c.CallID + ":" + c.Answer)
}

// shortDir is a temp directory whose PATH fits in a unix socket address. t.TempDir() on macOS is
// about ninety bytes before the file name, which is past the limit — the tests would fail on the
// path length rather than on anything they mean to check.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "magid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// start runs a daemon on a temp socket and returns a connected client.
func start(t *testing.T, eng Engine) *Client {
	t.Helper()
	path := filepath.Join(shortDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, eng, path) }()
	var c *Client
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		if c, err = Dial(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("the daemon never came up")
	}
	t.Cleanup(func() {
		c.Close()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve returned %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Serve did not stop when its context was cancelled")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("the socket file outlived the daemon")
		}
	})
	return c
}

// The five calls cross, with what they carry.
//
// These are the only things that CANNOT be done in a second process: they touch the goroutine
// driving the run, its cancel, and the tool blocked waiting for an answer. Everything else the
// screen asks is a read both sides can do from the same store.
func TestTheFiveCallsCrossTheSocket(t *testing.T) {
	eng := &fakeEngine{}
	c := start(t, eng)
	ctx := context.Background()
	sid := session.SessionID("s_1")
	parts := []session.Part{{Kind: session.PartText, Text: "do the thing"}}

	for _, err := range []error{
		c.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts}),
		c.Steer(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts}),
		c.Interrupt(ctx, command.Interrupt{SessionID: sid}),
		c.RespondPermission(ctx, command.RespondPermission{SessionID: sid, CallID: "c1", Decision: "always"}),
		c.RespondQuestion(ctx, command.RespondQuestion{SessionID: sid, CallID: "q1", Answer: "yes"}),
	} {
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
	}
	want := []string{
		"submit:s_1:do the thing", "steer:s_1:do the thing", "interrupt:s_1",
		"permission:c1:always", "answer:q1:yes",
	}
	got := eng.seen()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the engine saw\n  %v\nwant\n  %v", got, want)
	}
}

// The engine's error text reaches the caller. A client told only "it failed" cannot tell a rejected
// session id from a dead engine, and would retry both.
func TestTheEnginesReasonComesBack(t *testing.T) {
	eng := &fakeEngine{fail: errors.New("no such session")}
	c := start(t, eng)
	err := c.Interrupt(context.Background(), command.Interrupt{SessionID: "gone"})
	if err == nil {
		t.Fatal("a failing call reported success")
	}
	if !strings.Contains(err.Error(), "no such session") {
		t.Errorf("the reason did not cross: %v", err)
	}
	// And the connection survives it — a UI that asked for something impossible should be told,
	// not disconnected.
	if err := c.Interrupt(context.Background(), command.Interrupt{SessionID: "s_1"}); err == nil ||
		!strings.Contains(err.Error(), "no such session") {
		t.Errorf("the connection did not survive a rejected call: %v", err)
	}
}

// The socket is owner-only. Anything that can write to it can make this engine act, in this
// workspace, with this workspace's permissions.
func TestTheSocketIsOwnerOnly(t *testing.T) {
	path := filepath.Join(shortDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	go Serve(ctx, &fakeEngine{}, path)
	defer cancel()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil {
			if perm := fi.Mode().Perm(); perm != 0o600 {
				t.Errorf("socket mode is %o, want 600", perm)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the socket never appeared")
}

// A socket file left by a killed daemon does not block the next one. SIGKILL leaves it behind, and
// refusing to start over a path nobody is listening on would need a manual delete every crash.
func TestAStaleSocketDoesNotBlockAStart(t *testing.T) {
	path := filepath.Join(shortDir(t), "d.sock")
	if err := os.WriteFile(path, []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Serve(ctx, &fakeEngine{}, path)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := Dial(path); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a stale socket file kept the daemon from starting")
}

// Two daemons on one workspace is two engines writing one store. The second must refuse.
func TestASecondDaemonRefusesTheSameSocket(t *testing.T) {
	path := filepath.Join(shortDir(t), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Serve(ctx, &fakeEngine{}, path)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := Dial(path); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	err := Serve(context.Background(), &fakeEngine{}, path)
	if err == nil {
		t.Fatal("a second daemon took over a live socket")
	}
	// "another magi": the claim on the path is what refuses now, and it refuses before anything
	// looks at the socket — so the reason names the other PROCESS rather than the file it holds.
	// The listening-based wording still exists for a daemon that predates the claim (see
	// TestAListenerWithNoClaimIsStillRefused).
	if !strings.Contains(err.Error(), "another magi") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// Dialling nothing says what to do about it.
func TestDiallingNoDaemonSaysHowToStartOne(t *testing.T) {
	_, err := Dial(filepath.Join(shortDir(t), "nothing.sock"))
	if err == nil {
		t.Fatal("dialling a socket that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), "--daemon") {
		t.Errorf("the error does not say how to start one: %v", err)
	}
}

// An unknown method names what IS accepted — a client told only "unknown" cannot tell a typo from
// a version skew, and those want different reactions.
func TestAnUnknownMethodNamesTheOnesThatWork(t *testing.T) {
	eng := &fakeEngine{}
	c := start(t, eng)
	err := c.call(Request{Method: "explode"})
	if err == nil {
		t.Fatal("an unknown method was accepted")
	}
	for _, m := range []string{"submit", "steer", "interrupt", "permission", "answer"} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("the error does not list %q: %v", m, err)
		}
	}
}

// Two workspaces get two sockets, and the path stays inside what a unix socket allows.
func TestEachWorkspaceGetsItsOwnSocket(t *testing.T) {
	cfg := shortDir(t)
	a := SocketPath(cfg, "/some/deep/path/project-one")
	b := SocketPath(cfg, "/some/other/place/project-one") // same base name, different tree
	if a == b {
		t.Error("two workspaces share one socket — one daemon would answer for the other")
	}
	for _, p := range []string{a, b} {
		if len(p) > 100 {
			t.Errorf("socket path is %d bytes, past what macOS allows: %s", len(p), p)
		}
		if !strings.Contains(p, "project-one") {
			t.Errorf("the path is not recognisable to a person looking for theirs: %s", p)
		}
	}
}

// One directory has one socket, however you got to it.
//
// Go's os.Getwd prefers $PWD when it points at the same place, so a shell that did `cd /tmp/x`
// reports the logical path while a process that chdir'd itself reports /private/tmp/x. Same
// directory, two hashes, and the attach says "no daemon here" while one is running. Found by
// running it.
func TestOneDirectoryHasOneSocketHoweverYouReachedIt(t *testing.T) {
	real, err := os.MkdirTemp("/tmp", "magiws")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(real)
	// /tmp is a symlink to /private/tmp on macOS; on Linux the two are the same path and this
	// degenerates to comparing a path with itself, which still has to hold.
	logical := "/tmp/" + filepath.Base(real)

	cfg := shortDir(t)
	if a, b := SocketPath(cfg, logical), SocketPath(cfg, real); a != b {
		t.Errorf("the same directory reached two ways gives two sockets:\n  %s\n  %s", a, b)
	}
}

// The one read that crosses, and why it has to.
//
// Everything else a screen wants is in the log, which both processes share. A prompt the engine is
// blocked on is not: it is a question about what should happen next, so it is never written down,
// and the transient event announcing it is delivered to the bus of the process that is stuck. From
// outside, that agent looks exactly like one running a slow build — and it is the one case where
// looking away costs you the run.
func TestStatusCarriesWhatTheDaemonIsBlockedOn(t *testing.T) {
	eng := &fakeEngine{}
	c := start(t, eng)

	// Nothing pending: the answer is "nothing", not an error and not a guess.
	w, err := c.Status("s_1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if w != nil {
		t.Fatalf("an unblocked daemon reported %+v", w)
	}

	since := time.Now().Add(-90 * time.Second).UTC().Truncate(time.Second)
	eng.mu.Lock()
	eng.waiting = &app.Ask{Kind: "permission", What: "bash", Since: since}
	eng.mu.Unlock()

	w, err = c.Status("s_1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if w == nil {
		t.Fatal("a blocked daemon reported nothing")
	}
	if w.Kind != "permission" || w.What != "bash" {
		t.Errorf("status carried %+v, want permission on bash", w)
	}
	// The time it has been waiting is the difference between "it just asked" and "it has been
	// stuck since you went to lunch", so it has to survive the wire.
	got, perr := time.Parse(time.RFC3339, w.Since)
	if perr != nil {
		t.Fatalf("since %q does not parse: %v", w.Since, perr)
	}
	if !got.Equal(since) {
		t.Errorf("since came back as %s, want %s", got, since)
	}

	// A status request must not be mistaken for a command: nothing reaches the engine's writes.
	if seen := eng.seen(); len(seen) != 0 {
		t.Errorf("asking for status ran %v on the engine", seen)
	}
}

// controllingEngine is an engine that also accepts the calls which change how it runs.
type controllingEngine struct {
	fakeEngine
	rewound  int
	compacts int
	model    string
	perm     string
}

func (c *controllingEngine) Rewind(_ context.Context, _ session.SessionID, n int) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rewound = n
	return int64(n), nil
}
func (c *controllingEngine) Compact(context.Context, command.Compact) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compacts++
	return nil
}
func (c *controllingEngine) SetModel(_ session.SessionID, m string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = m
}
func (c *controllingEngine) SetPermission(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.perm = p
}

// The calls that change how the daemon runs have to reach the daemon.
//
// A viewer holds its own throwaway App, so doing these locally changed a copy nobody was using —
// the screen showed a new model while the daemon kept generating with the old one. Rewind and
// Compact were worse: they rewrite the log the daemon owns, under a process whose sequence counter
// knows nothing about it.
func TestTheControlCallsReachTheEngine(t *testing.T) {
	eng := &controllingEngine{}
	c := start(t, eng)
	ctx := context.Background()
	sid := session.SessionID("s_1")

	if _, err := c.Rewind(ctx, sid, 2); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if err := c.Compact(ctx, command.Compact{SessionID: sid}); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if err := c.SetModel(sid, "gpt-oss:120b-cloud"); err != nil {
		t.Fatalf("model: %v", err)
	}
	if err := c.SetPermission("allow"); err != nil {
		t.Fatalf("permission: %v", err)
	}

	eng.mu.Lock()
	rewound, compacts, model, perm := eng.rewound, eng.compacts, eng.model, eng.perm
	eng.mu.Unlock() // seen() takes the same lock, and holding it across the call is a deadlock
	if rewound != 2 || compacts != 1 || model != "gpt-oss:120b-cloud" || perm != "allow" {
		t.Errorf("the engine saw rewind=%d compact=%d model=%q perm=%q", rewound, compacts, model, perm)
	}
	// Answering a prompt and setting the policy are different things and must stay different words
	// on the wire: one decides a call, the other decides every call.
	if got := eng.seen(); len(got) != 0 {
		t.Errorf("a control call was delivered as a write: %v", got)
	}
}

// An engine that cannot be controlled says so, rather than accepting and dropping it.
func TestADaemonWithoutAControllerRefusesTheseCalls(t *testing.T) {
	c := start(t, &fakeEngine{}) // the five writes only
	err := c.SetModel("s_1", "something-else")
	if err == nil {
		t.Fatal("a daemon with no control surface accepted a model change")
	}
	if !strings.Contains(err.Error(), "cannot be controlled") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// cronEngine is an engine that also holds scheduled work.
type cronEngine struct {
	Engine
	mu      sync.Mutex
	reloads int
}

func (c *cronEngine) ReloadCron() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reloads++
}

func (c *cronEngine) seenReloads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reloads
}

// serveOn runs a daemon the test can watch stop, which start() cannot: it owns the cancel and the
// assertions about how Serve ended, and that is exactly what these tests are about.
func serveOn(t *testing.T, eng Engine) (path string, served <-chan error) {
	t.Helper()
	path = filepath.Join(shortDir(t), "d.sock")
	d, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { ch <- d.Serve(context.Background(), eng) }()
	return path, ch
}

func dialWhenUp(t *testing.T, path string) *Client {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := Dial(path); err == nil {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon never came up")
	return nil
}

// Stopping a companion is stopping the daemon that IS the companion. The record, the socket and the
// schedule go together: a record removed while its daemon kept running would leave work happening
// that nothing on any screen could account for.
func TestADaemonAskedToStopStopsAndAnswersFirst(t *testing.T) {
	path, served := serveOn(t, &fakeEngine{})
	cl := dialWhenUp(t, path)

	// The reply has to arrive. Stopping before answering would close the listener with this write
	// still pending, and the client would see a dropped connection — which is also what a daemon
	// that crashed on being asked looks like.
	if err := cl.Shutdown(); err != nil {
		t.Fatalf("shutdown was not acknowledged: %v", err)
	}
	cl.Close()

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve returned %v, want nil — an asked-for stop is not a failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve kept running after being asked to stop")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the socket is still at %s, advertising a daemon that is gone", path)
	}
}

func TestAnEditorInAnotherProcessCanSayTheScheduleChanged(t *testing.T) {
	eng := &cronEngine{Engine: &fakeEngine{}}
	path, served := serveOn(t, eng)
	cl := dialWhenUp(t, path)

	if err := cl.ReloadCron(); err != nil {
		t.Fatalf("reload-cron: %v", err)
	}
	if got := eng.seenReloads(); got != 1 {
		t.Errorf("the engine was told %d times, want 1", got)
	}
	_ = cl.Shutdown()
	cl.Close()
	<-served
}

// An engine holding no scheduled work refuses rather than answering OK to something it did not do.
func TestADaemonWithoutSchedulingSaysSo(t *testing.T) {
	path, served := serveOn(t, &fakeEngine{})
	cl := dialWhenUp(t, path)

	if err := cl.ReloadCron(); err == nil {
		t.Error("an engine holding no scheduled work answered OK to reload-cron")
	}
	_ = cl.Shutdown()
	cl.Close()
	<-served
}

// A client that asks to stop and then holds its connection open must not keep the daemon alive.
//
// It did. The stop was deferred to the end of the connection handler, so it waited for the peer to
// hang up — and the test that was meant to cover shutdown closed the client, so it passed. The
// mutation that should have broken it (stopping before answering) changed nothing, which is how the
// real fault surfaced: the ordering the code claimed to care about was not the thing holding it up.
func TestShutdownDoesNotWaitForTheClientToHangUp(t *testing.T) {
	path, served := serveOn(t, &fakeEngine{})
	cl := dialWhenUp(t, path)
	defer cl.Close() // deliberately AFTER the assertion below

	if err := cl.Shutdown(); err != nil {
		t.Fatalf("shutdown was not acknowledged: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon is still running while the client that stopped it holds the connection")
	}
}

// A connection accepted in the instant before Stop ran still gets closed.
//
// The window is real and cannot be scheduled from outside: Accept returns, Stop closes the listener
// and walks the connections it knows about, and only then does the new one get added — with nothing
// left to close it. Serve would wait on that handler forever. So the interleaving is called
// directly rather than raced for, which is the only way to make it deterministic.
func TestAConnectionAcceptedJustAsItStopsIsClosedAnyway(t *testing.T) {
	d := &Daemon{stop: make(chan struct{})}
	d.Stop() // the walk happens here, over an empty set

	server, client := net.Pipe()
	defer client.Close()
	d.track(server) // the late arrival

	// Distinguished by WHICH error, not by whether there was one. A closed pipe end fails its write
	// at once; a live one blocks on the unread pipe and fails on the deadline — both non-nil, so an
	// "err != nil" assertion here passes either way and proves nothing. It did, until this was
	// deliberately broken and stayed green.
	_ = server.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	_, err := server.Write([]byte("x"))
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("a connection accepted as the daemon stopped was left open (write gave %v); "+
			"Serve would wait on its handler forever", err)
	}
}
