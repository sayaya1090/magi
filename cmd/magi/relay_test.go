package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
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
