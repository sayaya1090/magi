package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
)

// publishFake writes a daemon's record and optionally puts something behind the socket. serve nil
// leaves a record with no listener at all — which is what a SIGKILL leaves behind.
func publishFake(t *testing.T, dir, name, sid string, serve func(net.Listener)) string {
	t.Helper()
	sock := filepath.Join(dir, "daemon-"+name+".sock")
	if serve != nil {
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go serve(ln)
	}
	b, err := json.Marshal(Info{Socket: sock, Workdir: "/w/" + name, Session: sid, PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SessionFile(sock), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return sock
}

// acceptSilently is alive in every way a file can show, and answers nothing: the accept loop runs
// and the connection is held open on purpose (closing it would answer, with EOF). A wedged process,
// a paused one, a stopped debugger.
func acceptSilently(ln net.Listener) {
	var held []net.Conn
	for {
		c, err := ln.Accept()
		if err != nil {
			for _, h := range held {
				h.Close()
			}
			return
		}
		held = append(held, c)
	}
}

// waitForSocket blocks until a listener is accepting, so a test does not race the daemon it started.
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", path); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no daemon came up at %s", path)
}

// A listing must survive the thing it exists to report.
//
// The probe went from a plain dial to a dial plus a question, and a question waits for an answer. A
// daemon that accepts and never replies then holds the listing forever — so `magi --agents` and the
// dashboard hang precisely when one process is wedged, which is the moment somebody runs them.
// Observed exactly that way, in the code that had just been written to show it.
func TestListSurvivesADaemonThatDoesNotAnswer(t *testing.T) {
	dir := shortDir(t)
	publishFake(t, dir, "wedged", "s_wedged", acceptSilently)

	// A healthy daemon alongside it: the point is that its entry is still correct and still
	// arrives, not merely that the call returns.
	healthy := publishFake(t, dir, "healthy", "s_healthy", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Serve(ctx, &fakeEngine{waiting: &app.Ask{ID: "c1", Kind: "permission", What: "bash"}}, healthy)
	}()
	waitForSocket(t, healthy)

	start := time.Now()
	done := make(chan []Info, 1)
	go func() { l, _ := List(dir); done <- l }()
	select {
	case list := <-done:
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("List took %s — it waited on the wedged daemon far past the probe bound", took)
		}
		if len(list) != 2 {
			t.Fatalf("List returned %d entries, want 2", len(list))
		}
		for _, in := range list {
			switch in.Session {
			case "s_wedged":
				// It accepted the connection, so it IS alive; what it would not do is answer. The
				// entry keeps the true half rather than dropping the whole row.
				if !in.Live {
					t.Error("a daemon that accepted the connection was reported dead")
				}
				if in.Asking != nil {
					t.Error("a daemon that never answered reported something it is waiting for")
				}
			case "s_healthy":
				if !in.Live || in.Asking == nil || in.Asking.What != "bash" {
					t.Errorf("the healthy daemon came back as %+v — one wedged neighbour cost its answer", in)
				}
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("List hung on a daemon that accepts and does not answer")
	}
}

// A socket file whose owner is gone must not cost the probe bound either. This is the ordinary case
// after a SIGKILL, and it is what most entries in a long-lived config directory look like.
func TestListIsQuickWhenNobodyIsHome(t *testing.T) {
	dir := shortDir(t)
	for i := 0; i < 8; i++ {
		sock := publishFake(t, dir, fmt.Sprintf("dead%d", i), fmt.Sprintf("s_%d", i), nil)
		if err := os.WriteFile(sock, nil, 0o600); err != nil { // the file SIGKILL leaves behind
			t.Fatal(err)
		}
	}
	start := time.Now()
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 8 {
		t.Fatalf("List returned %d entries, want 8", len(list))
	}
	for _, in := range list {
		if in.Live {
			t.Errorf("%s has no listener and was reported live", in.Socket)
		}
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("listing 8 dead sockets took %s", took)
	}
}

// Twenty instances was the open question this whole view came from. Probed serially a listing costs
// the SUM of every daemon's latency and one slow answer delays every entry behind it; in parallel
// it costs the slowest, which the probe bound caps.
func TestListProbesInParallel(t *testing.T) {
	dir := shortDir(t)
	const n = 12
	for i := 0; i < n; i++ {
		publishFake(t, dir, fmt.Sprintf("slow%d", i), fmt.Sprintf("s_%d", i), acceptSilently)
	}
	start := time.Now()
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if len(list) != n {
		t.Fatalf("List returned %d entries, want %d", len(list), n)
	}
	// Serial would be n × the bound. Parallel is one bound plus scheduling; past three bounds they
	// are queueing behind each other.
	if limit := 3 * probeTimeout; took > limit {
		t.Errorf("probing %d unresponsive daemons took %s, more than %s — they are not concurrent",
			n, took, limit)
	}
}

// A probe's deadline must not leak onto a UI's connection. The viewer's client holds one connection
// for the life of the page; a deadline set once and never cleared would make every later call fail
// with a timeout that has nothing to do with the daemon.
func TestAViewersConnectionHasNoDeadline(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "daemon-ui.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, &fakeEngine{}, sock) }()
	waitForSocket(t, sock)

	cl, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if cl.deadline != 0 {
		t.Fatalf("a UI connection carries a %s deadline", cl.deadline)
	}
	// Several calls spread past any plausible bound: a deadline that was set on the connection
	// (rather than per exchange) would fire on one of these.
	for i := 0; i < 3; i++ {
		if _, _, _, err := cl.Status("s_1"); err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		time.Sleep(probeTimeout / 2)
	}
}
