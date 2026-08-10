package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
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
	t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, describingEngine{}, sock) }()
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

// ssh is the default and a person can say otherwise, per host.
//
// A container has no sshd and its hostname is an id nothing resolves, so the cluster is unreachable
// from there however clever this gets. What makes it reachable is somebody saying once how to get
// in — and that saying stays HERE. A command line arriving over the network is code this process
// would run, so a reach is never gossiped and never copied by --join.
func TestHowToReachAMachineIsSaidHereOrItIsSsh(t *testing.T) {
	none := config.Config{}
	name, args := reachFor(none, "buildbox", "/run/d.sock")
	if name != "ssh" {
		t.Fatalf("the default is %q, not ssh", name)
	}
	if !strings.Contains(strings.Join(args, " "), "--relay /run/d.sock") {
		t.Errorf("the default does not relay to the socket: %v", args)
	}

	told := config.Config{Reach: map[string]config.Reach{
		"agent-b": {Command: "docker", Args: []string{"exec", "-i", "{{host}}", "magi", "--relay", "{{socket}}"}},
	}}
	name, args = reachFor(told, "agent-b", "/run/x.sock")
	if name != "docker" {
		t.Fatalf("what this machine was told was ignored: %q", name)
	}
	if strings.Join(args, " ") != "exec -i agent-b magi --relay /run/x.sock" {
		t.Errorf("the template was not filled in: %v", args)
	}
	// A host it was told nothing about is still ssh, so one container does not change the rest.
	if n, _ := reachFor(told, "buildbox", "/run/d.sock"); n != "ssh" {
		t.Errorf("a host with no entry became %q", n)
	}
}

// Only the two names are substituted, by exact spelling.
//
// A general expansion over a command line is how a socket path that happens to contain a brace
// becomes something else — and this builds a command that then runs.
func TestOnlyTheTwoNamesAreFilledIn(t *testing.T) {
	got := fillReach("a{{host}}b{{socket}}c{{whatever}}d", "H", "S")
	if want := "aHbSc{{whatever}}d"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
