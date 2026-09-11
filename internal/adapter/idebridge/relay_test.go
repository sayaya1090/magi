package idebridge

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/shortdir"
)

// relayFixture starts a listener and the relay against it, and hands back both ends.
func relayFixture(t *testing.T) (conn net.Conn, write *io.PipeWriter, output *io.PipeReader, done chan int) {
	t.Helper()
	dir, err := shortdir.Make("relay-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	input, write := io.Pipe()
	output, read := io.Pipe()
	t.Cleanup(func() { _ = input.Close(); _ = write.Close(); _ = output.Close(); _ = read.Close() })
	done = make(chan int, 1)
	go func() { done <- relay(socket, input, read, io.Discard) }()
	conn, err = listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn, write, output, done
}

// Bytes cross both ways, unchanged.
func TestRelayCarriesBothDirectionsUnchanged(t *testing.T) {
	conn, write, output, _ := relayFixture(t)
	request := []byte("{\"method\":\"transcript\"}\n")
	go func() { _, _ = write.Write(request) }()
	got := make([]byte, len(request))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, request) {
		t.Fatalf("request changed: %q", got)
	}
	frames := []byte("{\"ok\":true}\n{\"ok\":true,\"event\":{}}\n")
	go func() { _, _ = conn.Write(frames) }()
	got = make([]byte, len(frames))
	if _, err := io.ReadFull(output, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, frames) {
		t.Fatalf("frames changed: %q", got)
	}
}

// A caller that finishes writing still gets its answer.
//
// ⚠ **This test used to assert the opposite, and the opposite was a defect.** It required that
// stdin's EOF end the relay and tear the daemon connection down ("daemon connection survived
// EOF"), with no reason written beside it — and that is not what the socket this relay stands in
// for does. Measured 2026-09-11 on one socket in one run:
//
//	direct connection, CloseWrite after the request  → the reply arrives
//	through the relay, stdin closed after the request → nothing, exit 0
//
// And the shape a person reaches for first is exactly that one:
//
//	echo '{"method":"about"}' | magi ide-bridge --raw-socket <sock>   → empty stdout, exit 0
//
// The relay exists to BE a unix socket for a runtime that cannot open one (Node on Windows; see
// clients/vscode/src/core/daemon.ts). Being strictly weaker than what it stands in for is the
// defect — the caller did nothing wrong, and the failure says nothing at all.
//
// So stdin's end is a half-close now, and what ends the relay is the daemon closing. The daemon
// must still SEE the end: a door that reads to EOF is waiting for it.
func TestACallerThatFinishedWritingStillGetsItsAnswer(t *testing.T) {
	conn, write, output, done := relayFixture(t)
	go func() { _, _ = write.Write([]byte("{\"method\":\"about\"}\n")) }()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("the request did not arrive: %v", err)
	}
	if line != "{\"method\":\"about\"}\n" {
		t.Fatalf("the request changed: %q", line)
	}

	// The caller is done writing. The daemon sees that, and answers afterwards.
	_ = write.Close()
	half := make([]byte, 1)
	if _, err := conn.Read(half); err != io.EOF {
		t.Fatalf("the daemon did not see the request stream end: %v", err)
	}
	go func() { _, _ = conn.Write([]byte("{\"ok\":true,\"out\":\"answered\"}\n")) }()

	// Read with a deadline, not just a read. If the relay tears the connection down on stdin's EOF
	// — the defect this test is about — nothing ever closes the pipe it was writing into, so a
	// plain read here waits for ever. A guard that hangs instead of failing says nothing and takes
	// the whole package's timeout with it. Measured: reverting the fix hung this test until the
	// package deadline, with no message.
	type answer struct {
		s   string
		err error
	}
	got := make(chan answer, 1)
	go func() { s, e := bufio.NewReader(output).ReadString('\n'); got <- answer{s, e} }()
	select {
	case g := <-got:
		if g.err != nil {
			t.Fatalf("the answer was lost after the caller stopped writing: %v", g.err)
		}
		if g.s != "{\"ok\":true,\"out\":\"answered\"}\n" {
			t.Fatalf("the answer changed: %q", g.s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no answer within 3s after the caller stopped writing — the relay closed the " +
			"connection on stdin's EOF instead of half-closing it")
	}

	// And the daemon closing is what ends it.
	_ = conn.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the relay did not stop when the daemon closed")
	}
}
