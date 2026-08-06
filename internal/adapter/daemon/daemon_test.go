package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

type fakeEngine struct {
	mu   sync.Mutex
	got  []string
	fail error
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
	if !strings.Contains(err.Error(), "already listening") {
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
